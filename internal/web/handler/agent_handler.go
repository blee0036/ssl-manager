package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// AgentHandler handles HTTP requests for Agent API endpoints.
type AgentHandler struct {
	machineService     *service.MachineService
	mcService          *service.MachineCertificateService
	deployLogService   *service.DeploymentLogService
	certRepo           *repository.CertificateRepository
	mcRepo             *repository.MachineCertificateRepository
	alertSender        middleware.AgentAlertSender
	versionCache       *service.VersionCache
}

// NewAgentHandler creates a new AgentHandler.
// versionCache is optional (can be nil) for backward compatibility.
func NewAgentHandler(
	machineService *service.MachineService,
	mcService *service.MachineCertificateService,
	deployLogService *service.DeploymentLogService,
	certRepo *repository.CertificateRepository,
	mcRepo *repository.MachineCertificateRepository,
	alertSender middleware.AgentAlertSender,
	versionCache ...*service.VersionCache,
) *AgentHandler {
	var vc *service.VersionCache
	if len(versionCache) > 0 {
		vc = versionCache[0]
	}
	return &AgentHandler{
		machineService:   machineService,
		mcService:        mcService,
		deployLogService: deployLogService,
		certRepo:         certRepo,
		mcRepo:           mcRepo,
		alertSender:      alertSender,
		versionCache:     vc,
	}
}

// RegisterRoutes registers agent routes on the given chi router.
// All routes require Agent Token authentication via AgentAuthMiddleware.
func (h *AgentHandler) RegisterRoutes(r chi.Router, machineRepo middleware.MachineRepository, alertSender middleware.AgentAlertSender, auditRepo middleware.AuditRepository) {
	r.Route("/api/agent", func(r chi.Router) {
		// All agent routes require agent token authentication
		r.Use(middleware.AgentAuthMiddleware(machineRepo, alertSender))
		r.Use(middleware.AuditMiddleware(auditRepo))

		r.Post("/heartbeat", h.Heartbeat)
		r.Get("/machines/{machine_id}/certificates", h.ListMachineCertificates)
		r.Get("/machine-certificates/{machine_certificate_id}/download", h.DownloadCertificate)
		r.Post("/deployment-logs", h.CreateDeploymentLog)
	})
}

// Heartbeat handles POST /api/agent/heartbeat
// Updates the machine's last heartbeat time, agent version, hostname, IP, OS, and arch.
// If versionCache is available, includes latest version info in the response.
func (h *AgentHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	// Get machine from context (set by AgentAuthMiddleware)
	machine := middleware.GetMachine(r.Context())
	if machine == nil {
		writeErrorResponse(w, http.StatusUnauthorized, "unauthorized", "")
		return
	}

	var info model.HeartbeatInfo
	if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	// Use the machine_id from the authenticated context, not from the request body
	info.MachineID = machine.ID

	if err := h.machineService.UpdateHeartbeat(r.Context(), machine.ID, info); err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to update heartbeat", err.Error())
		return
	}

	// Build response with optional version info
	response := map[string]interface{}{
		"status":  "ok",
		"message": "heartbeat received",
	}

	// If versionCache is available, include latest version info
	if h.versionCache != nil && info.OS != "" && info.Arch != "" {
		release, found := h.versionCache.GetRelease(info.OS, info.Arch)
		if found {
			version := h.versionCache.GetVersion()
			if version != "" {
				response["latest_version"] = version
				response["md5"] = release.MD5
				response["download_url"] = release.DownloadURL
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// ListMachineCertificates handles GET /api/agent/machines/{machine_id}/certificates
// Returns the list of machine certificate configs for the authenticated machine.
// Validates that the machine_id in the URL matches the authenticated machine's ID.
func (h *AgentHandler) ListMachineCertificates(w http.ResponseWriter, r *http.Request) {
	machine := middleware.GetMachine(r.Context())
	if machine == nil {
		writeErrorResponse(w, http.StatusUnauthorized, "unauthorized", "")
		return
	}

	machineID := chi.URLParam(r, "machine_id")
	if machineID != machine.ID {
		writeErrorResponse(w, http.StatusForbidden, "machine_id does not match authenticated machine", "")
		return
	}

	mcs, err := h.mcService.GetByMachineID(r.Context(), machineID)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to get machine certificates", err.Error())
		return
	}

	// Build response with certificate fingerprints
	type certConfigResponse struct {
		MachineCertificateID string `json:"machine_certificate_id"`
		CertificateID        string `json:"certificate_id"`
		FingerprintSHA256    string `json:"fingerprint_sha256"`
		CertPath             string `json:"cert_path"`
		PrivateKeyPath       string `json:"private_key_path"`
		PostDeployCommands   string `json:"post_deploy_commands"`
		ConfigRevision       int    `json:"config_revision"`
		LastDeployStatus     string `json:"last_deploy_status"`
	}

	results := make([]certConfigResponse, 0, len(mcs))
	for _, mc := range mcs {
		// Get the certificate to retrieve the fingerprint
		cert, err := h.certRepo.GetByID(r.Context(), mc.CertificateID)
		fingerprint := ""
		if err == nil && cert != nil {
			fingerprint = cert.FingerprintSHA256
		}

		results = append(results, certConfigResponse{
			MachineCertificateID: mc.ID,
			CertificateID:       mc.CertificateID,
			FingerprintSHA256:    fingerprint,
			CertPath:             mc.CertPath,
			PrivateKeyPath:       mc.PrivateKeyPath,
			PostDeployCommands:   mc.PostDeployCommands,
			ConfigRevision:       mc.ConfigRevision,
			LastDeployStatus:     mc.LastDeployStatus,
		})
	}

	writeSuccessResponse(w, http.StatusOK, "ok", results)
}

// DownloadCertificate handles GET /api/agent/machine-certificates/{machine_certificate_id}/download
// Downloads the certificate files (fullchain.pem + privkey.pem).
// MUST verify that the machine_certificate belongs to the authenticated machine (security requirement 16.2).
func (h *AgentHandler) DownloadCertificate(w http.ResponseWriter, r *http.Request) {
	machine := middleware.GetMachine(r.Context())
	if machine == nil {
		writeErrorResponse(w, http.StatusUnauthorized, "unauthorized", "")
		return
	}

	machineCertID := chi.URLParam(r, "machine_certificate_id")

	// Get the machine certificate record
	mc, err := h.mcRepo.GetByID(r.Context(), machineCertID)
	if err != nil {
		if isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "machine certificate not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to get machine certificate", err.Error())
		return
	}

	// Security check: verify the machine_certificate belongs to the authenticated machine
	if mc.MachineID != machine.ID {
		writeErrorResponse(w, http.StatusForbidden, "machine certificate does not belong to this machine", "")
		return
	}

	// Get the certificate to retrieve the fingerprint
	cert, err := h.certRepo.GetByID(r.Context(), mc.CertificateID)
	if err != nil {
		if isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "certificate not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to get certificate", err.Error())
		return
	}

	// Read certificate files from disk
	_, _, fullchainPEM, privkeyPEM, err := h.certRepo.ReadCertFiles(cert.ID)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to read certificate files", err.Error())
		return
	}

	if fullchainPEM == nil || privkeyPEM == nil {
		writeErrorResponse(w, http.StatusNotFound, "certificate files not found", "")
		return
	}

	resp := model.AgentCertDownloadResponse{
		CertificateID:     cert.ID,
		FingerprintSHA256: cert.FingerprintSHA256,
		FullchainPEM:      string(fullchainPEM),
		PrivateKeyPEM:     string(privkeyPEM),
	}

	writeSuccessResponse(w, http.StatusOK, "ok", resp)
}

// createDeploymentLogRequest is the request body for POST /api/agent/deployment-logs.
type createDeploymentLogRequest struct {
	MachineCertificateID  string               `json:"machine_certificate_id"`
	CertificateID         string               `json:"certificate_id"`
	Status                string               `json:"status"`
	CertFingerprintSHA256 string               `json:"cert_fingerprint_sha256"`
	CertPath              string               `json:"cert_path"`
	PrivateKeyPath        string               `json:"private_key_path"`
	CommandOutputs        []model.CommandOutput `json:"command_outputs"`
	ErrorMessage          string               `json:"error_message"`
	StartedAt             time.Time            `json:"started_at"`
	FinishedAt            time.Time            `json:"finished_at"`
}

// CreateDeploymentLog handles POST /api/agent/deployment-logs
// Receives deployment log from Agent, saves it, and updates machine_certificate's deploy status.
func (h *AgentHandler) CreateDeploymentLog(w http.ResponseWriter, r *http.Request) {
	machine := middleware.GetMachine(r.Context())
	if machine == nil {
		writeErrorResponse(w, http.StatusUnauthorized, "unauthorized", "")
		return
	}

	var req createDeploymentLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	// Validate required fields
	if req.MachineCertificateID == "" || req.Status == "" {
		writeErrorResponse(w, http.StatusBadRequest, "machine_certificate_id and status are required", "")
		return
	}

	// Validate status value
	if req.Status != "success" && req.Status != "failed" && req.Status != "skipped" {
		writeErrorResponse(w, http.StatusBadRequest, "status must be one of: success, failed, skipped", "")
		return
	}

	// Verify the machine_certificate belongs to this machine
	mc, err := h.mcRepo.GetByID(r.Context(), req.MachineCertificateID)
	if err != nil {
		if isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "machine certificate not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to get machine certificate", err.Error())
		return
	}

	if mc.MachineID != machine.ID {
		writeErrorResponse(w, http.StatusForbidden, "machine certificate does not belong to this machine", "")
		return
	}

	// Create the deployment log
	log := &model.DeploymentLog{
		MachineCertificateID:  req.MachineCertificateID,
		MachineID:             machine.ID,
		CertificateID:         req.CertificateID,
		Status:                req.Status,
		CertFingerprintSHA256: req.CertFingerprintSHA256,
		CertPath:              req.CertPath,
		PrivateKeyPath:        req.PrivateKeyPath,
		CommandOutputs:        req.CommandOutputs,
		ErrorMessage:          req.ErrorMessage,
		StartedAt:             req.StartedAt,
		FinishedAt:            req.FinishedAt,
	}

	if err := h.deployLogService.Create(r.Context(), log); err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to create deployment log", err.Error())
		return
	}

	// Send alert if deployment failed
	if req.Status == "failed" && h.alertSender != nil {
		alertContent := fmt.Sprintf(
			"Certificate deployment failed on machine %s for certificate %s. Error: %s",
			machine.Name, req.CertificateID, req.ErrorMessage,
		)
		_ = h.alertSender.SendAlert(
			r.Context(), "critical", "deploy_failed",
			"Certificate Deployment Failed",
			alertContent, "machine", machine.ID,
		)
	}

	// Auto-resolve deploy_failed alert on successful deployment
	if req.Status == "success" && h.alertSender != nil {
		h.alertSender.AutoResolve(r.Context(), "machine", machine.ID, "deploy_failed")
	}

	// Update machine_certificate's last_deploy_status, last_deploy_at, last_deploy_message
	if err := h.mcRepo.UpdateDeployStatus(r.Context(), req.MachineCertificateID, req.Status, req.ErrorMessage); err != nil {
		// Log the error but don't fail the request since the log was already saved
		writeSuccessResponse(w, http.StatusOK, "deployment log saved, but failed to update deploy status", nil)
		return
	}

	writeSuccessResponse(w, http.StatusOK, "deployment log saved", nil)
}
