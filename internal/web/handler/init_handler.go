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
//
// NOTE: These are registered as individual routes (not via r.Route("/init", ...))
// on purpose. r.Route()/Mount() would also install a catch-all handler on the
// bare "/init" path itself (with no sub-path), which intercepts GET /init before
// it can fall through to the SPA's "/*" fallback. That caused the frontend's own
// "/init" client-side route (the init wizard page) to be shadowed by a raw 404
// from the backend instead of being served index.html. Registering exact
// sub-paths avoids creating any handler on bare "/init", so it falls through to
// the SPA handler as expected.
func (h *InitHandler) RegisterRoutes(r chi.Router) {
	r.Get("/init/status", h.GetStatus)
	r.Post("/init/admin", h.CreateAdmin)
	r.Post("/init/config", h.SaveConfig)
}

// GetStatus returns the current initialization status.
// Returns 200 with phase info if system needs initialization.
// Returns 403 if system is already fully initialized.
func (h *InitHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	phase, err := h.initService.GetPhase(r.Context())
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to check initialization status", "")
		return
	}

	if phase == "completed" {
		writeErrorResponse(w, http.StatusForbidden, "system is already initialized", "")
		return
	}

	writeSuccessResponse(w, http.StatusOK, "system needs initialization", map[string]interface{}{
		"initialized": false,
		"phase":       phase,
	})
}

// CreateAdmin creates the first admin user during initialization.
// The service layer (InitService.CreateAdmin) is the single authority on whether creation
// is allowed — it handles completed/unexpired-pending/expired-pending cases within a transaction.
func (h *InitHandler) CreateAdmin(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var input service.CreateAdminInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	// Create admin user — service handles all state checks within a transaction
	user, initToken, err := h.initService.CreateAdmin(r.Context(), input)
	if err != nil {
		switch err {
		case service.ErrAlreadyInitialized:
			writeErrorResponse(w, http.StatusForbidden, "system is already initialized", "")
		case service.ErrInitPendingNotExpired:
			writeErrorResponse(w, http.StatusConflict, err.Error(), "")
		case service.ErrUsernameRequired, service.ErrPasswordRequired, service.ErrPasswordTooShort:
			writeErrorResponse(w, http.StatusBadRequest, err.Error(), "")
		default:
			// Internal errors (DB, crypto, transaction) — don't leak details
			writeErrorResponse(w, http.StatusInternalServerError, "internal error during admin creation", "")
		}
		return
	}

	writeSuccessResponse(w, http.StatusCreated, "admin user created", map[string]interface{}{
		"id":         user.ID,
		"username":   user.Username,
		"role":       user.Role,
		"init_token": initToken,
	})
}

// SaveConfig saves the system configuration during initialization.
// Requires X-Init-Token header for authentication.
// Returns 403 if token is missing, invalid, or expired.
func (h *InitHandler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	// Extract and validate init token BEFORE parsing body
	initToken := r.Header.Get("X-Init-Token")
	if initToken == "" {
		writeErrorResponse(w, http.StatusForbidden, "invalid init token", "")
		return
	}

	// Parse request body
	var input service.SaveConfigInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	// Save config (full token hash validation happens inside the service)
	cfg, err := h.initService.SaveConfig(r.Context(), initToken, input)
	if err != nil {
		switch err {
		case service.ErrAlreadyInitialized:
			writeErrorResponse(w, http.StatusForbidden, "system is already initialized", "")
		case service.ErrInitNotComplete:
			writeErrorResponse(w, http.StatusBadRequest, "admin user must be created first", "")
		case service.ErrInvalidInitToken:
			writeErrorResponse(w, http.StatusForbidden, "invalid init token", "")
		case service.ErrInitTokenExpired:
			writeErrorResponse(w, http.StatusForbidden, "init token expired", "")
		default:
			writeErrorResponse(w, http.StatusInternalServerError, err.Error(), "")
		}
		return
	}

	// Mask sensitive fields before returning
	maskedCfg := *cfg
	if maskedCfg.Readonly.ViewPassword != "" {
		maskedCfg.Readonly.ViewPassword = "***"
	}
	if maskedCfg.Turnstile.SecretKey != "" {
		maskedCfg.Turnstile.SecretKey = "***"
	}

	writeSuccessResponse(w, http.StatusOK, "configuration saved", &maskedCfg)
}
