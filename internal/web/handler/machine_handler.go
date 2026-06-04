package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// MachineHandler handles HTTP requests for machine management.
type MachineHandler struct {
	machineService *service.MachineService
}

// NewMachineHandler creates a new MachineHandler.
func NewMachineHandler(machineService *service.MachineService) *MachineHandler {
	return &MachineHandler{
		machineService: machineService,
	}
}

// RegisterRoutes registers machine routes on the given chi router.
// All routes require authentication. Write operations require admin or user role.
func (h *MachineHandler) RegisterRoutes(r chi.Router, authService middleware.AuthService, auditRepo middleware.AuditRepository) {
	r.Route("/api/machines", func(r chi.Router) {
		// All machine routes require authentication
		r.Use(middleware.AuthMiddleware(authService))
		r.Use(middleware.AuditMiddleware(auditRepo))
		r.Use(middleware.ReadonlyMiddleware())

		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.GetByID)
			r.Put("/", h.Update)
			r.Delete("/", h.Delete)
			r.Post("/revoke-token", h.RevokeToken)
			r.Post("/regenerate-token", h.RegenerateToken)
			r.Get("/install-command", h.GetInstallCommand)
		})
	})
}

// List handles GET /api/machines
func (h *MachineHandler) List(w http.ResponseWriter, r *http.Request) {
	filter := model.MachineFilter{
		Status: r.URL.Query().Get("status"),
		Search: r.URL.Query().Get("search"),
	}

	machines, err := h.machineService.List(r.Context(), filter)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to list machines", err.Error())
		return
	}

	if machines == nil {
		machines = []*model.Machine{}
	}

	writeSuccessResponse(w, http.StatusOK, "success", machines)
}

// Create handles POST /api/machines
func (h *MachineHandler) Create(w http.ResponseWriter, r *http.Request) {
	var input model.CreateMachineInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	machine, token, err := h.machineService.Create(r.Context(), input)
	if err != nil {
		if isValidationError(err) {
			writeErrorResponse(w, http.StatusBadRequest, err.Error(), "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to create machine", err.Error())
		return
	}

	// Set audit info with the newly created machine ID
	middleware.SetAuditInfo(r, middleware.AuditInfo{
		TargetType: "machine",
		TargetID:   machine.ID,
		Operation:  "create_machine",
	})

	// Return machine with the plaintext token (only shown once)
	// Also include the install command using the configured external_url
	installCmd, _ := h.machineService.GetInstallCommand(r.Context(), machine.ID, token)
	resp := map[string]interface{}{
		"machine":         machine,
		"agent_token":     token,
		"install_command": installCmd,
	}
	writeSuccessResponse(w, http.StatusCreated, "machine created", resp)
}

// GetByID handles GET /api/machines/{id}
func (h *MachineHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "machine id is required", "")
		return
	}

	machine, err := h.machineService.GetByID(r.Context(), id)
	if err != nil {
		if isNotFoundError(err) {
			writeErrorResponse(w, http.StatusNotFound, "machine not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to get machine", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "success", machine)
}

// Update handles PUT /api/machines/{id}
func (h *MachineHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "machine id is required", "")
		return
	}

	var input model.UpdateMachineInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	machine, err := h.machineService.Update(r.Context(), id, input)
	if err != nil {
		if isNotFoundError(err) {
			writeErrorResponse(w, http.StatusNotFound, "machine not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to update machine", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "machine updated", machine)
}

// Delete handles DELETE /api/machines/{id}
func (h *MachineHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "machine id is required", "")
		return
	}

	err := h.machineService.Delete(r.Context(), id)
	if err != nil {
		if isNotFoundError(err) {
			writeErrorResponse(w, http.StatusNotFound, "machine not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to delete machine", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "machine deleted", nil)
}

// RevokeToken handles POST /api/machines/{id}/revoke-token
func (h *MachineHandler) RevokeToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "machine id is required", "")
		return
	}

	err := h.machineService.RevokeToken(r.Context(), id)
	if err != nil {
		if isNotFoundError(err) {
			writeErrorResponse(w, http.StatusNotFound, "machine not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to revoke token", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "token revoked", nil)
}

// RegenerateToken handles POST /api/machines/{id}/regenerate-token
func (h *MachineHandler) RegenerateToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "machine id is required", "")
		return
	}

	token, err := h.machineService.GenerateToken(r.Context(), id)
	if err != nil {
		if isNotFoundError(err) {
			writeErrorResponse(w, http.StatusNotFound, "machine not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to regenerate token", err.Error())
		return
	}

	resp := map[string]interface{}{
		"agent_token": token,
	}
	// Also include the install command using the configured external_url
	installCmd, _ := h.machineService.GetInstallCommand(r.Context(), id, token)
	resp["install_command"] = installCmd
	writeSuccessResponse(w, http.StatusOK, "token regenerated", resp)
}

// GetInstallCommand handles GET /api/machines/{id}/install-command
// Returns the install command template. The user must first regenerate the token
// via POST /api/machines/{id}/regenerate-token to get a fresh token for the command.
// This endpoint does NOT regenerate the token to avoid write side-effects on GET.
func (h *MachineHandler) GetInstallCommand(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "machine id is required", "")
		return
	}

	// Get the install command template without a token.
	// The user must regenerate the token separately.
	cmd, err := h.machineService.GetInstallCommand(r.Context(), id, "<AGENT_TOKEN>")
	if err != nil {
		if isNotFoundError(err) {
			writeErrorResponse(w, http.StatusNotFound, "machine not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to generate install command", err.Error())
		return
	}

	resp := map[string]interface{}{
		"install_command": cmd,
		"note":           "Use POST /api/machines/{id}/regenerate-token to get a fresh agent_token, then replace <AGENT_TOKEN> in the command.",
	}
	writeSuccessResponse(w, http.StatusOK, "success", resp)
}


