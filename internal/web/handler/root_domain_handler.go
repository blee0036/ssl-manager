package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// DNSConfigResolver resolves a stored Cloudflare API token from an existing
// third-party DNS configuration by its id. It is satisfied by
// *service.ThirdpartDNSService (which exposes GetConfig), keeping the dependency
// decoupled and easy to mock in tests.
//
// It is OPTIONAL: when a RootDomainHandler is constructed with a nil resolver,
// only the api_token import path is supported and an import request that provides
// only config_id is rejected with HTTP 400.
type DNSConfigResolver interface {
	GetConfig(ctx context.Context, id string) (*model.ThirdpartDNS, error)
}

// RootDomainHandler handles HTTP requests for root-domain (WHOIS registration
// expiry) monitoring. It is fully independent of DomainHandler (TLS certificate
// monitoring).
type RootDomainHandler struct {
	svc         *service.DomainExpiryService
	dnsResolver DNSConfigResolver
}

// NewRootDomainHandler creates a new RootDomainHandler.
//
// dnsResolver is optional and used only to resolve an api_token from a config_id
// on the import path (POST /api/root-domains/import). Pass a *service.ThirdpartDNSService
// to enable config_id imports; pass nil to support only the api_token path.
func NewRootDomainHandler(svc *service.DomainExpiryService, dnsResolver DNSConfigResolver) *RootDomainHandler {
	return &RootDomainHandler{
		svc:         svc,
		dnsResolver: dnsResolver,
	}
}

// RegisterRoutes registers root-domain routes on the given chi router.
// All routes require authentication. Write operations are blocked in readonly mode.
func (h *RootDomainHandler) RegisterRoutes(r chi.Router, authService middleware.AuthService, auditRepo middleware.AuditRepository) {
	r.Route("/api/root-domains", func(r chi.Router) {
		// All routes require authentication
		r.Use(middleware.AuthMiddleware(authService))
		r.Use(middleware.AuditMiddleware(auditRepo))
		r.Use(middleware.ReadonlyMiddleware())

		// Static routes must be registered before /{id} to avoid chi treating them as IDs
		r.Post("/import", h.Import)

		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.GetByID)
			r.Put("/", h.Update)
			r.Delete("/", h.Delete)
			r.Post("/refresh", h.Refresh)
		})
	})
}

// List handles GET /api/root-domains - list root domains with server-side
// sorting, filtering, and pagination. Response data is { items, total, page, per_page }.
func (h *RootDomainHandler) List(w http.ResponseWriter, r *http.Request) {
	params := model.RootDomainListParams{
		Name:         r.URL.Query().Get("name"),
		Source:       r.URL.Query().Get("source"),
		FilterStatus: r.URL.Query().Get("filter_status"),
		SortBy:       r.URL.Query().Get("sort_by"),
		SortOrder:    r.URL.Query().Get("sort_order"),
	}

	if me := r.URL.Query().Get("monitor_enabled"); me != "" {
		val := me == "true"
		params.MonitorEnabled = &val
	}
	if ai := r.URL.Query().Get("alert_ignored"); ai != "" {
		val := ai == "true"
		params.AlertIgnored = &val
	}

	params.Page = parseIntParam(r, "page", 1)
	params.PerPage = parseIntParam(r, "per_page", 50)

	items, total, err := h.svc.ListWithSort(r.Context(), params)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to list root domains", err.Error())
		return
	}

	if items == nil {
		items = []*model.RootDomain{}
	}

	// Return paginated response: { items, total, page, per_page }
	type PaginatedData struct {
		Items   []*model.RootDomain `json:"items"`
		Total   int                 `json:"total"`
		Page    int                 `json:"page"`
		PerPage int                 `json:"per_page"`
	}

	writeSuccessResponse(w, http.StatusOK, "success", PaginatedData{
		Items:   items,
		Total:   total,
		Page:    params.Page,
		PerPage: params.PerPage,
	})
}

// Create handles POST /api/root-domains - manually add a root domain (source="manual").
// The service makes a best-effort WHOIS refresh after creation and folds the
// result into the returned record, so no separate probe/refresh field is returned.
func (h *RootDomainHandler) Create(w http.ResponseWriter, r *http.Request) {
	var input model.CreateRootDomainInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	rd, err := h.svc.Create(r.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrValidation):
			writeErrorResponse(w, http.StatusBadRequest, "failed to create root domain", err.Error())
		case errors.Is(err, service.ErrDuplicate):
			writeErrorResponse(w, http.StatusConflict, "root domain already exists", err.Error())
		default:
			writeErrorResponse(w, http.StatusInternalServerError, "failed to create root domain", err.Error())
		}
		return
	}

	writeSuccessResponse(w, http.StatusCreated, "root domain created", rd)
}

// Import handles POST /api/root-domains/import - import root domains from Cloudflare.
// Body: { "api_token"?: string, "config_id"?: string }. When only config_id is
// provided, the api_token is resolved from the existing DNS config via the
// (optional) DNSConfigResolver.
func (h *RootDomainHandler) Import(w http.ResponseWriter, r *http.Request) {
	var input model.ImportRootDomainsInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	token := input.APIToken
	// If no token provided, resolve it from config_id via the DNS config resolver.
	if token == "" && input.ConfigID != "" {
		if h.dnsResolver == nil {
			writeErrorResponse(w, http.StatusBadRequest, "config_id is not supported", "no DNS config resolver configured")
			return
		}
		cfg, err := h.dnsResolver.GetConfig(r.Context(), input.ConfigID)
		if err != nil {
			// A missing / invalid config_id is a client input error (requirement 2.3).
			writeErrorResponse(w, http.StatusBadRequest, "failed to resolve config_id", err.Error())
			return
		}
		token = cfg.APIToken
	}

	if token == "" {
		writeErrorResponse(w, http.StatusBadRequest, "api_token or config_id required", "")
		return
	}

	result, err := h.svc.ImportFromCloudflare(r.Context(), token)
	if err != nil {
		// ScanZones failure (invalid token / network): descriptive error, set
		// unchanged (requirement 2.3). Aligns with the existing scan-zones handler.
		writeErrorResponse(w, http.StatusInternalServerError, "failed to import root domains", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "import completed", result)
}

// GetByID handles GET /api/root-domains/{id} - get root domain details.
func (h *RootDomainHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "root domain id is required", "")
		return
	}

	rd, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "root domain not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to get root domain", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "success", rd)
}

// Update handles PUT /api/root-domains/{id} - update a root domain's
// monitor_enabled / alert_ignored flags, and/or manually set or clear its
// registration expiry_date (for domains whose registry is structurally
// unqueryable via WHOIS/RDAP; see model.UpdateRootDomainInput.ExpiryDate).
// An invalid expiry_date string is a validation error mapped to HTTP 400.
func (h *RootDomainHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "root domain id is required", "")
		return
	}

	var input model.UpdateRootDomainInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	rd, err := h.svc.Update(r.Context(), id, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "root domain not found", "")
			return
		}
		writeErrorResponse(w, http.StatusBadRequest, "failed to update root domain", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "root domain updated", rd)
}

// Delete handles DELETE /api/root-domains/{id} - delete a root domain and its
// inlined registration-expiry data.
func (h *RootDomainHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "root domain id is required", "")
		return
	}

	err := h.svc.Delete(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "root domain not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to delete root domain", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "root domain deleted", nil)
}

// Refresh handles POST /api/root-domains/{id}/refresh - trigger a manual WHOIS
// refresh. WHOIS-layer failures do not surface as errors: the returned record has
// last_status="failed" / last_error set and the previously known expiry_date is
// preserved (requirement 7.2). Only a missing id (404) or infrastructure error
// (500) produces an error response.
func (h *RootDomainHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "root domain id is required", "")
		return
	}

	rd, err := h.svc.RefreshOne(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "root domain not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to refresh root domain", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "refresh completed", rd)
}
