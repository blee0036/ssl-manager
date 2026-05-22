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

// DomainHandler handles HTTP requests for domain monitoring.
type DomainHandler struct {
	domainService *service.DomainMonitorService
}

// NewDomainHandler creates a new DomainHandler.
func NewDomainHandler(domainService *service.DomainMonitorService) *DomainHandler {
	return &DomainHandler{
		domainService: domainService,
	}
}

// RegisterRoutes registers domain monitoring routes on the given chi router.
// All routes require authentication. Write operations are blocked in readonly mode.
func (h *DomainHandler) RegisterRoutes(r chi.Router, authService middleware.AuthService, auditRepo middleware.AuditRepository) {
	r.Route("/api/domains", func(r chi.Router) {
		// All domain routes require authentication
		r.Use(middleware.AuthMiddleware(authService))
		r.Use(middleware.AuditMiddleware(auditRepo))
		r.Use(middleware.ReadonlyMiddleware())

		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Get("/{id}", h.GetByID)
		r.Put("/{id}", h.Update)
		r.Delete("/{id}", h.Delete)
		r.Post("/{id}/probe", h.Probe)
	})
}

// List handles GET /api/domains - list domain monitors with optional filtering.
func (h *DomainHandler) List(w http.ResponseWriter, r *http.Request) {
	filter := model.DomainFilter{
		Name:           r.URL.Query().Get("name"),
		Source:         r.URL.Query().Get("source"),
		ThirdpartDNSID: r.URL.Query().Get("thirdpart_dns_id"),
	}

	if monitorEnabled := r.URL.Query().Get("monitor_enabled"); monitorEnabled != "" {
		val := monitorEnabled == "true"
		filter.MonitorEnabled = &val
	}

	domains, err := h.domainService.List(r.Context(), filter)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to list domains", err.Error())
		return
	}

	if domains == nil {
		domains = []*model.Domain{}
	}

	// Build response with latest monitor results
	type DomainWithMonitor struct {
		*model.Domain
		LatestMonitorResult *model.DomainMonitorResult `json:"latest_monitor_result"`
	}

	results := make([]DomainWithMonitor, 0, len(domains))
	for _, d := range domains {
		dwm := DomainWithMonitor{Domain: d}
		if result, err := h.domainService.GetLatestMonitorResult(r.Context(), d.ID); err == nil {
			dwm.LatestMonitorResult = result
		}
		results = append(results, dwm)
	}

	writeSuccessResponse(w, http.StatusOK, "success", results)
}

// Create handles POST /api/domains - create a new domain monitor.
func (h *DomainHandler) Create(w http.ResponseWriter, r *http.Request) {
	var input model.CreateDomainInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	// Validate required fields
	if input.Name == "" {
		writeErrorResponse(w, http.StatusBadRequest, "domain name is required", "")
		return
	}

	domain, err := h.domainService.Create(r.Context(), input)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "failed to create domain monitor", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusCreated, "domain monitor created", domain)
}

// GetByID handles GET /api/domains/{id} - get domain monitor details.
func (h *DomainHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "domain id is required", "")
		return
	}

	domain, err := h.domainService.GetByID(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "domain not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to get domain", err.Error())
		return
	}

	// Build response with latest monitor result
	type DomainWithMonitor struct {
		*model.Domain
		LatestMonitorResult *model.DomainMonitorResult `json:"latest_monitor_result"`
	}

	dwm := DomainWithMonitor{Domain: domain}
	if result, err := h.domainService.GetLatestMonitorResult(r.Context(), id); err == nil {
		dwm.LatestMonitorResult = result
	}

	writeSuccessResponse(w, http.StatusOK, "success", dwm)
}

// Update handles PUT /api/domains/{id} - update a domain monitor.
func (h *DomainHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "domain id is required", "")
		return
	}

	var input model.UpdateDomainInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	domain, err := h.domainService.Update(r.Context(), id, input)
	if err != nil {
		if err == sql.ErrNoRows || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "domain not found", "")
			return
		}
		writeErrorResponse(w, http.StatusBadRequest, "failed to update domain monitor", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "domain monitor updated", domain)
}

// Delete handles DELETE /api/domains/{id} - delete a domain monitor.
func (h *DomainHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "domain id is required", "")
		return
	}

	err := h.domainService.Delete(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "domain not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to delete domain monitor", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "domain monitor deleted", nil)
}

// Probe handles POST /api/domains/{id}/probe - trigger manual TLS probe.
func (h *DomainHandler) Probe(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "domain id is required", "")
		return
	}

	result, err := h.domainService.Probe(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows || isNotFoundError(err) || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "domain not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to probe domain", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "probe completed", result)
}
