package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// ThirdpartDNSHandler handles HTTP requests for third-party DNS upstream management.
type ThirdpartDNSHandler struct {
	dnsService *service.ThirdpartDNSService
}

// NewThirdpartDNSHandler creates a new ThirdpartDNSHandler.
func NewThirdpartDNSHandler(dnsService *service.ThirdpartDNSService) *ThirdpartDNSHandler {
	return &ThirdpartDNSHandler{
		dnsService: dnsService,
	}
}

// RegisterRoutes registers third-party DNS routes on the given chi router.
// All routes require authentication. Write operations are blocked in readonly mode.
func (h *ThirdpartDNSHandler) RegisterRoutes(r chi.Router, authService middleware.AuthService, auditRepo middleware.AuditRepository) {
	r.Route("/api/thirdpart-dns", func(r chi.Router) {
		// All routes require authentication
		r.Use(middleware.AuthMiddleware(authService))
		r.Use(middleware.AuditMiddleware(auditRepo))
		r.Use(middleware.ReadonlyMiddleware())

		// Static routes must be registered before /{id} to avoid chi treating them as IDs
		r.Post("/scan-zones", h.ScanZones)

		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.GetByID)
			r.Put("/", h.Update)
			r.Delete("/", h.Delete)
			r.Post("/sync", h.TriggerSync)
			r.Get("/sync-logs", h.GetSyncLogs)
		})
	})
}

// List handles GET /api/thirdpart-dns - list all DNS upstream configurations.
func (h *ThirdpartDNSHandler) List(w http.ResponseWriter, r *http.Request) {
	configs, err := h.dnsService.ListConfigs(r.Context())
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to list DNS configs", err.Error())
		return
	}

	if configs == nil {
		configs = []*model.ThirdpartDNS{}
	}

	writeSuccessResponse(w, http.StatusOK, "success", configs)
}

// Create handles POST /api/thirdpart-dns - create a new DNS upstream configuration.
// After successful creation, triggers an auto-sync (best-effort). Sync failure does not roll back the config.
func (h *ThirdpartDNSHandler) Create(w http.ResponseWriter, r *http.Request) {
	var input model.CreateThirdpartDNSInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	config, err := h.dnsService.CreateConfig(r.Context(), input)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "failed to create DNS config", err.Error())
		return
	}

	// After successful creation, trigger auto-sync (best-effort)
	syncResult, syncErr := h.dnsService.SyncRecords(r.Context(), config.ID)

	type CreateResponse struct {
		*model.ThirdpartDNS
		SyncResult *model.DNSSyncResult `json:"sync_result,omitempty"`
		SyncError  string               `json:"sync_error,omitempty"`
	}

	resp := CreateResponse{ThirdpartDNS: config}
	if syncErr != nil {
		resp.SyncError = syncErr.Error()
	} else {
		resp.SyncResult = syncResult
	}

	writeSuccessResponse(w, http.StatusCreated, "DNS config created", resp)
}

// GetByID handles GET /api/thirdpart-dns/{id} - get DNS upstream config details.
func (h *ThirdpartDNSHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "config id is required", "")
		return
	}

	config, err := h.dnsService.GetConfig(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "DNS config not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to get DNS config", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "success", config)
}

// Update handles PUT /api/thirdpart-dns/{id} - update a DNS upstream configuration.
func (h *ThirdpartDNSHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "config id is required", "")
		return
	}

	var input model.UpdateThirdpartDNSInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	config, err := h.dnsService.UpdateConfig(r.Context(), id, input)
	if err != nil {
		if err == sql.ErrNoRows || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "DNS config not found", "")
			return
		}
		writeErrorResponse(w, http.StatusBadRequest, "failed to update DNS config", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "DNS config updated", config)
}

// Delete handles DELETE /api/thirdpart-dns/{id} - delete a DNS upstream configuration.
func (h *ThirdpartDNSHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "config id is required", "")
		return
	}

	err := h.dnsService.DeleteConfig(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "DNS config not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to delete DNS config", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "DNS config deleted", nil)
}

// TriggerSync handles POST /api/thirdpart-dns/{id}/sync - trigger DNS record sync.
func (h *ThirdpartDNSHandler) TriggerSync(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "config id is required", "")
		return
	}

	result, err := h.dnsService.SyncRecords(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSyncInProgress):
			writeErrorResponse(w, http.StatusConflict, "sync in progress", err.Error())
		case errors.Is(err, service.ErrDNSConfigNotFound):
			writeErrorResponse(w, http.StatusNotFound, "config not found", err.Error())
		case errors.Is(err, service.ErrDNSConfigDisabled):
			writeErrorResponse(w, http.StatusBadRequest, "config disabled", err.Error())
		default:
			writeErrorResponse(w, http.StatusInternalServerError, "sync failed", err.Error())
		}
		return
	}

	writeSuccessResponse(w, http.StatusOK, "sync completed", result)
}

// GetSyncLogs handles GET /api/thirdpart-dns/{id}/sync-logs - get sync logs for a config.
func (h *ThirdpartDNSHandler) GetSyncLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "config id is required", "")
		return
	}

	page := parseIntParam(r, "page", 1)
	perPage := parseIntParam(r, "per_page", 50)

	logs, total, err := h.dnsService.GetSyncLogs(r.Context(), id, page, perPage)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to get sync logs", err.Error())
		return
	}

	if logs == nil {
		logs = []*model.ThirdpartDNSSyncLog{}
	}

	type PaginatedLogs struct {
		Items   []*model.ThirdpartDNSSyncLog `json:"items"`
		Total   int                          `json:"total"`
		Page    int                          `json:"page"`
		PerPage int                          `json:"per_page"`
	}

	writeSuccessResponse(w, http.StatusOK, "success", PaginatedLogs{
		Items:   logs,
		Total:   total,
		Page:    page,
		PerPage: perPage,
	})
}

// ScanZones handles POST /api/thirdpart-dns/scan-zones - scan available DNS zones using a token.
// Supports api_token or config_id (to look up the token from an existing config).
func (h *ThirdpartDNSHandler) ScanZones(w http.ResponseWriter, r *http.Request) {
	var input struct {
		APIToken string `json:"api_token"`
		ConfigID string `json:"config_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	token := input.APIToken
	// If no token provided, look up from config_id
	if token == "" && input.ConfigID != "" {
		cfg, err := h.dnsService.GetConfig(r.Context(), input.ConfigID)
		if err != nil {
			writeErrorResponse(w, http.StatusNotFound, "config not found", err.Error())
			return
		}
		token = cfg.APIToken
	}

	if token == "" {
		writeErrorResponse(w, http.StatusBadRequest, "api_token or config_id required", "")
		return
	}

	zones, err := h.dnsService.ScanZones(r.Context(), token)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to scan zones", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "success", zones)
}
