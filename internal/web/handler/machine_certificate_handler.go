package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// MachineCertificateHandler handles HTTP requests for machine certificate deployment configs.
type MachineCertificateHandler struct {
	mcService *service.MachineCertificateService
}

// NewMachineCertificateHandler creates a new MachineCertificateHandler.
func NewMachineCertificateHandler(mcService *service.MachineCertificateService) *MachineCertificateHandler {
	return &MachineCertificateHandler{
		mcService: mcService,
	}
}

// RegisterRoutes registers machine certificate routes on the given chi router.
// Routes are nested under /api/machines/{machine_id}/certificates.
// All routes require authentication. Write operations are blocked in readonly mode.
func (h *MachineCertificateHandler) RegisterRoutes(r chi.Router, authService middleware.AuthService, auditRepo middleware.AuditRepository) {
	r.Route("/api/machines/{machine_id}/certificates", func(r chi.Router) {
		r.Use(middleware.AuthMiddleware(authService))
		r.Use(middleware.AuditMiddleware(auditRepo))
		r.Use(middleware.ReadonlyMiddleware())

		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Route("/{mc_id}", func(r chi.Router) {
			r.Put("/", h.Update)
			r.Delete("/", h.Delete)
			r.Post("/deploy", h.TriggerDeploy)
		})
	})
}

// List handles GET /api/machines/{machine_id}/certificates
// Returns all deployment configs for the specified machine.
func (h *MachineCertificateHandler) List(w http.ResponseWriter, r *http.Request) {
	machineID := chi.URLParam(r, "machine_id")
	if machineID == "" {
		writeErrorResponse(w, http.StatusBadRequest, "machine_id is required", "")
		return
	}

	configs, err := h.mcService.GetByMachineID(r.Context(), machineID)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to list deployment configs", err.Error())
		return
	}

	if configs == nil {
		configs = []*model.MachineCertificate{}
	}

	writeSuccessResponse(w, http.StatusOK, "success", configs)
}

// Create handles POST /api/machines/{machine_id}/certificates
// Adds a new certificate deployment config for the specified machine.
func (h *MachineCertificateHandler) Create(w http.ResponseWriter, r *http.Request) {
	machineID := chi.URLParam(r, "machine_id")
	if machineID == "" {
		writeErrorResponse(w, http.StatusBadRequest, "machine_id is required", "")
		return
	}

	var input model.CreateMachineCertInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	// Override machine_id from URL path
	input.MachineID = machineID

	mc, err := h.mcService.Create(r.Context(), input)
	if err != nil {
		if isValidationError(err) {
			writeErrorResponse(w, http.StatusBadRequest, err.Error(), "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to create deployment config", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusCreated, "deployment config created", mc)
}

// Update handles PUT /api/machines/{machine_id}/certificates/{mc_id}
// Edits an existing certificate deployment config.
func (h *MachineCertificateHandler) Update(w http.ResponseWriter, r *http.Request) {
	mcID := chi.URLParam(r, "mc_id")
	if mcID == "" {
		writeErrorResponse(w, http.StatusBadRequest, "machine certificate id is required", "")
		return
	}

	var input model.UpdateMachineCertInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	mc, err := h.mcService.Update(r.Context(), mcID, input)
	if err != nil {
		if isNotFoundError(err) || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "deployment config not found", "")
			return
		}
		if isValidationError(err) {
			writeErrorResponse(w, http.StatusBadRequest, err.Error(), "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to update deployment config", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "deployment config updated", mc)
}

// Delete handles DELETE /api/machines/{machine_id}/certificates/{mc_id}
// Deletes a certificate deployment config.
func (h *MachineCertificateHandler) Delete(w http.ResponseWriter, r *http.Request) {
	mcID := chi.URLParam(r, "mc_id")
	if mcID == "" {
		writeErrorResponse(w, http.StatusBadRequest, "machine certificate id is required", "")
		return
	}

	err := h.mcService.Delete(r.Context(), mcID)
	if err != nil {
		if isNotFoundError(err) || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "deployment config not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to delete deployment config", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "deployment config deleted", nil)
}

// TriggerDeploy handles POST /api/machines/{machine_id}/certificates/{mc_id}/deploy
// Triggers a manual deployment for the specified machine certificate config.
func (h *MachineCertificateHandler) TriggerDeploy(w http.ResponseWriter, r *http.Request) {
	mcID := chi.URLParam(r, "mc_id")
	if mcID == "" {
		writeErrorResponse(w, http.StatusBadRequest, "machine certificate id is required", "")
		return
	}

	err := h.mcService.TriggerManualDeploy(r.Context(), mcID)
	if err != nil {
		if isNotFoundError(err) || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "deployment config not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to trigger deploy", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "deploy triggered", nil)
}
