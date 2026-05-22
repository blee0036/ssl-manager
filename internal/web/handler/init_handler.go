package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// InitHandler handles HTTP requests for the system initialization flow.
type InitHandler struct {
	initService *service.InitService
}

// NewInitHandler creates a new InitHandler.
func NewInitHandler(initService *service.InitService) *InitHandler {
	return &InitHandler{
		initService: initService,
	}
}

// RegisterRoutes registers the initialization routes on the given router.
func (h *InitHandler) RegisterRoutes(r chi.Router) {
	r.Route("/init", func(r chi.Router) {
		r.Get("/status", h.GetStatus)
		r.Post("/admin", h.CreateAdmin)
		r.Post("/config", h.SaveConfig)
	})
}

// GetStatus returns the current initialization status.
// Returns 200 with phase info if system needs initialization.
// Returns 403 if system is already fully initialized.
func (h *InitHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	fullyInit, err := h.initService.IsFullyInitialized(r.Context())
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to check initialization status", "")
		return
	}

	if fullyInit {
		writeErrorResponse(w, http.StatusForbidden, "system is already initialized", "")
		return
	}

	// Determine which phase we're in
	hasAdmin, err := h.initService.CheckInitialized(r.Context())
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to check initialization status", "")
		return
	}

	phase := "needs_admin"
	if hasAdmin {
		phase = "needs_config"
	}

	writeSuccessResponse(w, http.StatusOK, "system needs initialization", map[string]interface{}{
		"initialized": false,
		"phase":       phase,
	})
}

// CreateAdmin creates the first admin user during initialization.
// Returns 403 if admin user already exists.
func (h *InitHandler) CreateAdmin(w http.ResponseWriter, r *http.Request) {
	// Check if admin already exists
	hasAdmin, err := h.initService.CheckInitialized(r.Context())
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to check initialization status", "")
		return
	}
	if hasAdmin {
		writeErrorResponse(w, http.StatusForbidden, "admin user already exists", "")
		return
	}

	// Parse request body
	var input service.CreateAdminInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	// Create admin user
	user, err := h.initService.CreateAdmin(r.Context(), input)
	if err != nil {
		switch err {
		case service.ErrAlreadyInitialized:
			writeErrorResponse(w, http.StatusForbidden, "system is already initialized", "")
		default:
			writeErrorResponse(w, http.StatusBadRequest, err.Error(), "")
		}
		return
	}

	writeSuccessResponse(w, http.StatusCreated, "admin user created", map[string]interface{}{
		"id":       user.ID,
		"username": user.Username,
		"role":     user.Role,
	})
}

// SaveConfig saves the system configuration during initialization.
// Returns 403 if system is already fully initialized (config already exists).
func (h *InitHandler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var input service.SaveConfigInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	// Save config
	cfg, err := h.initService.SaveConfig(r.Context(), input)
	if err != nil {
		switch err {
		case service.ErrAlreadyInitialized:
			writeErrorResponse(w, http.StatusForbidden, "system is already initialized", "")
		case service.ErrInitNotComplete:
			writeErrorResponse(w, http.StatusBadRequest, "admin user must be created first", "")
		default:
			writeErrorResponse(w, http.StatusBadRequest, err.Error(), "")
		}
		return
	}

	// Mask sensitive fields before returning
	maskedCfg := *cfg
	if maskedCfg.Readonly.ViewPassword != "" {
		maskedCfg.Readonly.ViewPassword = "***"
	}

	writeSuccessResponse(w, http.StatusOK, "configuration saved", &maskedCfg)
}
