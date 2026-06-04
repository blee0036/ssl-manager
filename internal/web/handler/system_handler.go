package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
)

// SystemHandler handles HTTP requests for system configuration management.
type SystemHandler struct {
	configPath string
	runtimeCfg *config.RuntimeConfig
}

// NewSystemHandler creates a new SystemHandler.
func NewSystemHandler(configPath string, runtimeCfg *config.RuntimeConfig) *SystemHandler {
	return &SystemHandler{
		configPath: configPath,
		runtimeCfg: runtimeCfg,
	}
}

// RegisterRoutes registers system routes on the given chi router.
// GET is available to all authenticated users (including readonly); PUT requires admin role.
func (h *SystemHandler) RegisterRoutes(r chi.Router, authService middleware.AuthService, auditRepo middleware.AuditRepository) {
	r.Route("/api/system", func(r chi.Router) {
		r.Use(middleware.AuthMiddleware(authService))
		r.Use(middleware.AuditMiddleware(auditRepo))
		r.Use(middleware.ReadonlyMiddleware())

		r.Get("/config", h.GetConfig)
		r.Put("/config", h.UpdateConfig)
	})
}

// maskedValue is the placeholder used for sensitive fields.
const maskedValue = "***"

// maskConfig returns a copy of the config with sensitive fields masked.
func maskConfig(cfg *config.Config) *config.Config {
	masked := *cfg
	// Mask readonly view password
	if masked.Readonly.ViewPassword != "" {
		masked.Readonly.ViewPassword = maskedValue
	}
	// Mask turnstile secret key
	if masked.Turnstile.SecretKey != "" {
		masked.Turnstile.SecretKey = maskedValue
	}
	return &masked
}

// GetConfig handles GET /api/system/config
// Returns the current system configuration with sensitive fields masked.
func (h *SystemHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to load configuration", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "success", maskConfig(cfg))
}

// UpdateConfig handles PUT /api/system/config
// Updates the system configuration. Admin and user roles can perform this action.
func (h *SystemHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	// Check authenticated user (admin or user role)
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || (claims.Role != "admin" && claims.Role != "user") {
		writeErrorResponse(w, http.StatusForbidden, "access denied", "")
		return
	}

	// Parse the incoming config update
	var input config.Config
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	// Load existing config to preserve fields that are masked in the response
	existing, err := config.LoadConfig(h.configPath)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to load existing configuration", err.Error())
		return
	}

	// Preserve sensitive fields when client sends masked/empty values:
	// If the client sends "***", empty string, or the field is missing (zero value),
	// keep the existing value. Only replace if a new non-empty, non-masked value is provided.

	// readonly.view_password preservation
	if input.Readonly.ViewPassword == "" || input.Readonly.ViewPassword == maskedValue {
		input.Readonly.ViewPassword = existing.Readonly.ViewPassword
	}

	// turnstile.secret_key preservation
	if input.Turnstile.SecretKey == "" || input.Turnstile.SecretKey == maskedValue {
		input.Turnstile.SecretKey = existing.Turnstile.SecretKey
	}

	// Save the updated config
	if err := config.SaveConfig(h.configPath, &input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "failed to save configuration", err.Error())
		return
	}

	// Update the in-memory runtime config so all services see the new values
	h.runtimeCfg.Update(&input)

	// Return the saved config with sensitive fields masked
	writeSuccessResponse(w, http.StatusOK, "configuration updated", maskConfig(&input))
}
