package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/certbot"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// CertificateHandler handles HTTP requests for certificate management.
type CertificateHandler struct {
	certService    *service.CertificateService
	certbotWrapper *certbot.CertbotWrapper
	dnsRepo        ThirdpartDNSLookup
	mcCounter      MachineCertCounter
	dataDir        string // base data directory for checking private key existence
	certRenewer    CertificateRenewer
}

// ThirdpartDNSLookup defines the interface for looking up thirdpart DNS configs.
type ThirdpartDNSLookup interface {
	GetByID(ctx context.Context, id string) (*model.ThirdpartDNS, error)
}

// MachineCertCounter defines the interface for counting machine certificates per certificate.
type MachineCertCounter interface {
	CountByCertificateIDs(ctx context.Context, certIDs []string) (map[string]int, error)
}

// CertificateRenewer performs a direct renewal of an existing certificate.
type CertificateRenewer interface {
	RenewCertificate(ctx context.Context, certificateID string) (*model.Certificate, error)
}

// NewCertificateHandler creates a new CertificateHandler.
func NewCertificateHandler(certService *service.CertificateService, certbotWrapper *certbot.CertbotWrapper, dnsRepo ThirdpartDNSLookup, mcCounter MachineCertCounter, dataDir string) *CertificateHandler {
	return &CertificateHandler{
		certService:    certService,
		certbotWrapper: certbotWrapper,
		dnsRepo:        dnsRepo,
		mcCounter:      mcCounter,
		dataDir:        dataDir,
	}
}

// SetCertificateRenewer wires the scheduler-backed manual certificate renewer.
// It is a setter to keep existing handler construction compatible.
func (h *CertificateHandler) SetCertificateRenewer(renewer CertificateRenewer) {
	h.certRenewer = renewer
}

// RegisterRoutes registers certificate routes on the given chi router.
// All routes require authentication. Write operations are blocked in readonly mode.
func (h *CertificateHandler) RegisterRoutes(r chi.Router, authService middleware.AuthService, auditRepo middleware.AuditRepository) {
	r.Route("/api/certificates", func(r chi.Router) {
		// All certificate routes require authentication
		r.Use(middleware.AuthMiddleware(authService))
		r.Use(middleware.AuditMiddleware(auditRepo))
		r.Use(middleware.ReadonlyMiddleware())

		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Get("/{id}", h.GetByID)
		r.Put("/{id}", h.Update)
		r.Delete("/{id}", h.Delete)
		r.Post("/{id}/renew", h.Renew)

		// Certbot issuance endpoints
		r.Post("/issue/cloudflare", h.IssueCertbotCloudflare)
		r.Post("/issue/manual-dns/start", h.StartManualDNS)
		r.Post("/issue/manual-dns/complete", h.CompleteManualDNS)
	})
}

// List handles GET /api/certificates - list certificates with optional filtering.
func (h *CertificateHandler) List(w http.ResponseWriter, r *http.Request) {
	filter := model.CertFilter{
		Source: r.URL.Query().Get("source"),
	}

	if autoRenew := r.URL.Query().Get("auto_renew"); autoRenew != "" {
		val := autoRenew == "true"
		filter.AutoRenew = &val
	}

	if r.URL.Query().Get("expiring_soon") == "true" {
		filter.ExpiringSoon = true
	}

	certs, err := h.certService.List(r.Context(), filter)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to list certificates", err.Error())
		return
	}

	// Collect certificate IDs for batch machine count lookup
	certIDs := make([]string, 0, len(certs))
	for _, cert := range certs {
		certIDs = append(certIDs, cert.ID)
	}

	// Get machine counts per certificate
	machineCounts := make(map[string]int)
	if h.mcCounter != nil && len(certIDs) > 0 {
		counts, err := h.mcCounter.CountByCertificateIDs(r.Context(), certIDs)
		if err == nil {
			machineCounts = counts
		}
	}

	responses := make([]*model.CertificateResponse, 0, len(certs))
	for _, cert := range certs {
		resp := h.toCertificateResponse(cert)
		resp.MachineCount = machineCounts[cert.ID]
		responses = append(responses, resp)
	}

	writeSuccessResponse(w, http.StatusOK, "success", responses)
}

// Create handles POST /api/certificates - upload/create a new certificate.
func (h *CertificateHandler) Create(w http.ResponseWriter, r *http.Request) {
	var input model.CreateCertInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	// Validate required fields
	if input.Name == "" {
		writeErrorResponse(w, http.StatusBadRequest, "name is required", "")
		return
	}
	if len(input.CertPEM) == 0 {
		writeErrorResponse(w, http.StatusBadRequest, "cert_pem is required", "")
		return
	}
	if len(input.KeyPEM) == 0 {
		writeErrorResponse(w, http.StatusBadRequest, "key_pem is required", "")
		return
	}

	cert, err := h.certService.Create(r.Context(), input)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "failed to create certificate", err.Error())
		return
	}

	// Set audit info with the newly created certificate ID
	middleware.SetAuditInfo(r, middleware.AuditInfo{
		TargetType: "certificate",
		TargetID:   cert.ID,
		Operation:  "create_certificate",
	})

	writeSuccessResponse(w, http.StatusCreated, "certificate created", h.toCertificateResponse(cert))
}

// GetByID handles GET /api/certificates/{id} - get certificate details.
func (h *CertificateHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "certificate id is required", "")
		return
	}

	cert, err := h.certService.GetByID(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			writeErrorResponse(w, http.StatusNotFound, "certificate not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to get certificate", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "success", h.toCertificateResponse(cert))
}

// Update handles PUT /api/certificates/{id} - update a certificate.
func (h *CertificateHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "certificate id is required", "")
		return
	}

	var input model.UpdateCertInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	cert, err := h.certService.Update(r.Context(), id, input)
	if err != nil {
		if err == sql.ErrNoRows {
			writeErrorResponse(w, http.StatusNotFound, "certificate not found", "")
			return
		}
		writeErrorResponse(w, http.StatusBadRequest, "failed to update certificate", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "certificate updated", h.toCertificateResponse(cert))
}

// Delete handles DELETE /api/certificates/{id} - delete a certificate.
func (h *CertificateHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "certificate id is required", "")
		return
	}

	err := h.certService.Delete(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			writeErrorResponse(w, http.StatusNotFound, "certificate not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to delete certificate", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "certificate deleted", nil)
}

// Renew handles POST /api/certificates/{id}/renew.
// Only certificates originally issued with Certbot + Cloudflare DNS can be
// renewed directly because their DNS credentials are persisted by the system.
func (h *CertificateHandler) Renew(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "certificate id is required", "")
		return
	}
	if h.certRenewer == nil {
		writeErrorResponse(w, http.StatusServiceUnavailable, "certificate renewal is not configured", "")
		return
	}

	cert, err := h.certRenewer.RenewCertificate(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeErrorResponse(w, http.StatusNotFound, "certificate not found", "")
		case errors.Is(err, service.ErrManualRenewalUnsupported):
			writeErrorResponse(w, http.StatusBadRequest, "certificate does not support direct renewal", err.Error())
		case errors.Is(err, service.ErrCertificateRenewalInProgress):
			writeErrorResponse(w, http.StatusConflict, "certificate renewal is already in progress", "")
		default:
			writeErrorResponse(w, http.StatusInternalServerError, "certificate renewal failed", err.Error())
		}
		return
	}

	middleware.SetAuditInfo(r, middleware.AuditInfo{
		TargetType: "certificate",
		TargetID:   cert.ID,
		Operation:  "renew_certificate",
	})

	writeSuccessResponse(w, http.StatusOK, "certificate renewed", h.toCertificateResponse(cert))
}

// toCertificateResponse converts a Certificate model to a CertificateResponse DTO.
// The response does NOT include private_key_pem or file paths (per requirement 16.9).
func (h *CertificateHandler) toCertificateResponse(cert *model.Certificate) *model.CertificateResponse {
	return &model.CertificateResponse{
		ID:                cert.ID,
		Name:              cert.Name,
		Domains:           cert.Domains,
		Source:            cert.Source,
		ExpireAt:          cert.ExpireAt,
		AutoRenew:         cert.AutoRenew,
		Issuer:            cert.Issuer,
		FingerprintSHA256: cert.FingerprintSHA256,
		ChainValid:        cert.ChainValid,
		HasPrivateKey:     h.hasPrivateKey(cert.ID),
		LastRenewAt:       cert.LastRenewAt,
		RenewStatus:       cert.RenewStatus,
		CreatedAt:         cert.CreatedAt,
		UpdatedAt:         cert.UpdatedAt,
	}
}

// hasPrivateKey checks if a private key file exists for the given certificate ID.
func (h *CertificateHandler) hasPrivateKey(certID string) bool {
	privkeyPath := filepath.Join(h.dataDir, "certificates", certID, "privkey.pem")
	_, err := os.Stat(privkeyPath)
	return err == nil
}

// CertbotIssueInput holds the input for issuing a certificate via Certbot + Cloudflare DNS.
type CertbotIssueInput struct {
	Name           string   `json:"name"`
	Domains        []string `json:"domains"`
	Email          string   `json:"email"`
	ThirdpartDNSID string   `json:"thirdpart_dns_id"`
	AutoRenew      bool     `json:"auto_renew"`
}

// IssueCertbotCloudflare handles POST /api/certificates/issue/cloudflare
// Issues a certificate via Certbot + Cloudflare DNS-01 challenge.
// The Cloudflare API token is looked up from the thirdpart_dns record.
func (h *CertificateHandler) IssueCertbotCloudflare(w http.ResponseWriter, r *http.Request) {
	var input CertbotIssueInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if len(input.Domains) == 0 {
		writeErrorResponse(w, http.StatusBadRequest, "at least one domain is required", "")
		return
	}
	if input.ThirdpartDNSID == "" {
		writeErrorResponse(w, http.StatusBadRequest, "thirdpart_dns_id is required", "")
		return
	}
	if input.Name == "" {
		input.Name = input.Domains[0]
	}

	if h.certbotWrapper == nil {
		writeErrorResponse(w, http.StatusInternalServerError, "certbot is not configured", "")
		return
	}

	// Look up the thirdpart_dns record to get the API token
	if h.dnsRepo == nil {
		writeErrorResponse(w, http.StatusInternalServerError, "DNS repository is not configured", "")
		return
	}
	dnsConfig, err := h.dnsRepo.GetByID(r.Context(), input.ThirdpartDNSID)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "thirdpart_dns config not found", err.Error())
		return
	}
	if !dnsConfig.Enabled {
		writeErrorResponse(w, http.StatusBadRequest, "thirdpart_dns config is disabled", "")
		return
	}
	if dnsConfig.Type != "cloudflare" {
		writeErrorResponse(w, http.StatusBadRequest, "thirdpart_dns config must be of type 'cloudflare'", "")
		return
	}

	cloudflareToken := dnsConfig.APIToken

	// Issue certificate via Certbot
	result, err := h.certbotWrapper.IssueCertCloudflare(r.Context(), input.Domains, input.Email, cloudflareToken)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "certbot issuance failed", err.Error())
		return
	}

	// Save the certificate to the system with source=certbot_cloudflare_dns directly
	certInput := model.CreateCertInput{
		Name:           input.Name,
		CertPEM:        result.CertFiles.CertPEM,
		ChainPEM:       result.CertFiles.ChainPEM,
		KeyPEM:         result.CertFiles.PrivateKeyPEM,
		AutoRenew:      input.AutoRenew,
		ThirdpartDNSID: input.ThirdpartDNSID,
		Source:         "certbot_cloudflare_dns",
	}

	cert, err := h.certService.Create(r.Context(), certInput)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to save issued certificate", err.Error())
		return
	}

	// Set audit info with the newly created certificate ID
	middleware.SetAuditInfo(r, middleware.AuditInfo{
		TargetType: "certificate",
		TargetID:   cert.ID,
		Operation:  "issue_certificate_cloudflare",
	})

	writeSuccessResponse(w, http.StatusCreated, "certificate issued via certbot", h.toCertificateResponse(cert))
}

// ManualDNSStartInput holds the input for starting a manual DNS challenge.
type ManualDNSStartInput struct {
	Name      string   `json:"name"`
	Domains   []string `json:"domains"`
	Email     string   `json:"email"`
	AutoRenew bool     `json:"auto_renew"`
}

// ManualDNSCompleteInput holds the input for completing a manual DNS challenge.
type ManualDNSCompleteInput struct {
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
	AutoRenew bool   `json:"auto_renew"`
}

// StartManualDNS handles POST /api/certificates/issue/manual-dns/start
// Starts the manual DNS-01 challenge flow by running certbot with a blocking auth-hook.
// Returns the real ACME challenge values that the user must create as DNS TXT records.
func (h *CertificateHandler) StartManualDNS(w http.ResponseWriter, r *http.Request) {
	var input ManualDNSStartInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if len(input.Domains) == 0 {
		writeErrorResponse(w, http.StatusBadRequest, "at least one domain is required", "")
		return
	}

	if h.certbotWrapper == nil {
		writeErrorResponse(w, http.StatusInternalServerError, "certbot is not configured", "")
		return
	}

	// Start the manual DNS challenge flow
	session, err := h.certbotWrapper.StartManualDNSChallenge(r.Context(), input.Domains, input.Email)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to start manual DNS challenge", err.Error())
		return
	}

	// Return session ID and challenges to the user
	response := map[string]interface{}{
		"session_id": session.ID,
		"domains":    session.Domains,
		"challenges": session.Challenges,
		"message":    "Create the DNS TXT records listed in challenges, then call the complete endpoint with the session_id.",
	}

	writeSuccessResponse(w, http.StatusOK, "manual DNS challenge started", response)
}

// CompleteManualDNS handles POST /api/certificates/issue/manual-dns/complete
// Signals certbot to verify the DNS records and complete certificate issuance.
func (h *CertificateHandler) CompleteManualDNS(w http.ResponseWriter, r *http.Request) {
	var input ManualDNSCompleteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if input.SessionID == "" {
		writeErrorResponse(w, http.StatusBadRequest, "session_id is required", "")
		return
	}

	if h.certbotWrapper == nil {
		writeErrorResponse(w, http.StatusInternalServerError, "certbot is not configured", "")
		return
	}

	// Look up the session to get domain info for naming
	session, ok := h.certbotWrapper.GetSession(input.SessionID)
	if !ok {
		writeErrorResponse(w, http.StatusNotFound, "session not found or expired", "")
		return
	}

	// Determine certificate name
	certName := input.Name
	if certName == "" && session != nil {
		certName = session.Domains[0]
	}

	// Complete the challenge - signals certbot to verify DNS and issue cert
	result, err := h.certbotWrapper.CompleteManualDNSChallenge(input.SessionID)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "manual DNS challenge failed", err.Error())
		return
	}

	// Save the certificate to the system
	certInput := model.CreateCertInput{
		Name:      certName,
		CertPEM:   result.CertFiles.CertPEM,
		ChainPEM:  result.CertFiles.ChainPEM,
		KeyPEM:    result.CertFiles.PrivateKeyPEM,
		AutoRenew: input.AutoRenew,
		Source:    "certbot_manual_dns",
	}

	cert, err := h.certService.Create(r.Context(), certInput)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to save issued certificate", err.Error())
		return
	}

	// Set audit info with the newly created certificate ID
	middleware.SetAuditInfo(r, middleware.AuditInfo{
		TargetType: "certificate",
		TargetID:   cert.ID,
		Operation:  "issue_certificate_manual_dns",
	})

	writeSuccessResponse(w, http.StatusCreated, "certificate issued via manual DNS", h.toCertificateResponse(cert))
}
