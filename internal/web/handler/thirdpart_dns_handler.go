package handler

import (
	"database/sql"
	"encoding/json"
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

		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Get("/{id}", h.GetByID)
		r.Put("/{id}", h.Update)
		r.Delete("/{id}", h.Delete)
		r.Post("/{id}/sync", h.TriggerSync)
		r.Get("/{id}/sync-logs", h.GetSyncLogs)
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

	writeSuccessResponse(w, http.StatusCreated, "DNS config created", config)
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
		if err == sql.ErrNoRows || isNoRowsError(err) || isNotFoundError(err) {
			writeErrorResponse(w, http.StatusNotFound, "DNS config not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to sync DNS records", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "DNS sync completed", result)
}

// GetSyncLogs handles GET /api/thirdpart-dns/{id}/sync-logs - get sync logs for a config.
func (h *ThirdpartDNSHandler) GetSyncLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "config id is required", "")
		return
	}

	logs, err := h.dnsService.GetSyncLogs(r.Context(), id)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to get sync logs", err.Error())
		return
	}

	if logs == nil {
		logs = []*model.ThirdpartDNSSyncLog{}
	}

	writeSuccessResponse(w, http.StatusOK, "success", logs)
}
