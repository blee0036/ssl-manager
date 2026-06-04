package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/config"
)

// TurnstileConfigResponse is the response for GET /api/auth/turnstile-config.
// It intentionally excludes SecretKey to prevent leaking sensitive data to the frontend.
type TurnstileConfigResponse struct {
	Enabled bool   `json:"enabled"`
	SiteKey string `json:"site_key"`
}

// TurnstileHandler handles Turnstile configuration requests.
type TurnstileHandler struct {
	runtimeCfg *config.RuntimeConfig
}

// NewTurnstileHandler creates a new TurnstileHandler.
func NewTurnstileHandler(runtimeCfg *config.RuntimeConfig) *TurnstileHandler {
	return &TurnstileHandler{runtimeCfg: runtimeCfg}
}

// RegisterRoutes registers Turnstile routes on the given chi router.
// This endpoint does not require authentication — the frontend needs it before login.
func (h *TurnstileHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/auth/turnstile-config", h.GetConfig)
}

// GetConfig handles GET /api/auth/turnstile-config.
// Returns the Turnstile enabled status and site key for the frontend.
// Never returns the secret_key (Codex constraint #4).
// Uses RuntimeConfig to always reflect the latest configuration (Codex constraint #2).
func (h *TurnstileHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.runtimeCfg.Get()
	writeSuccessResponse(w, http.StatusOK, "success", TurnstileConfigResponse{
		Enabled: cfg.Turnstile.Enabled,
		SiteKey: cfg.Turnstile.SiteKey,
	})
}
