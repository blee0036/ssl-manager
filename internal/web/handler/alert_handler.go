package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// AlertHandler handles HTTP requests for alert management and notification channels.
type AlertHandler struct {
	alertService *service.AlertService
}

// NewAlertHandler creates a new AlertHandler.
func NewAlertHandler(alertService *service.AlertService) *AlertHandler {
	return &AlertHandler{
		alertService: alertService,
	}
}

// RegisterRoutes registers alert routes on the given chi router.
// All routes require authentication. Write operations are blocked in readonly mode.
func (h *AlertHandler) RegisterRoutes(r chi.Router, authService middleware.AuthService, auditRepo middleware.AuditRepository) {
	r.Route("/api/alerts", func(r chi.Router) {
		// All alert routes require authentication
		r.Use(middleware.AuthMiddleware(authService))
		r.Use(middleware.AuditMiddleware(auditRepo))
		r.Use(middleware.ReadonlyMiddleware())

		// Alert history
		r.Get("/", h.ListAlerts)
		r.Get("/{id}", h.GetAlert)
		r.Post("/{id}/resolve", h.ResolveAlert)

		// Notification channels
		r.Get("/channels", h.ListChannels)
		r.Post("/channels", h.CreateChannel)
		r.Put("/channels/{id}", h.UpdateChannel)
		r.Delete("/channels/{id}", h.DeleteChannel)
		r.Post("/channels/{id}/test", h.TestChannel)
	})
}

// ListAlerts handles GET /api/alerts - list alert history with optional filters.
func (h *AlertHandler) ListAlerts(w http.ResponseWriter, r *http.Request) {
	filter := model.AlertFilter{
		Level:  r.URL.Query().Get("level"),
		Type:   r.URL.Query().Get("type"),
		Status: r.URL.Query().Get("status"),
	}

	alerts, err := h.alertService.GetHistory(r.Context(), filter)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to list alerts", err.Error())
		return
	}

	if alerts == nil {
		alerts = []*model.Alert{}
	}

	writeSuccessResponse(w, http.StatusOK, "success", alerts)
}

// GetAlert handles GET /api/alerts/{id} - get alert details.
func (h *AlertHandler) GetAlert(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "alert id is required", "")
		return
	}

	// Avoid matching the "channels" sub-route as an ID
	if id == "channels" {
		h.ListChannels(w, r)
		return
	}

	alert, err := h.alertService.GetByID(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "alert not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to get alert", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "success", alert)
}

// ResolveAlert handles POST /api/alerts/{id}/resolve - mark alert as resolved.
func (h *AlertHandler) ResolveAlert(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "alert id is required", "")
		return
	}

	err := h.alertService.MarkResolved(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows || isNoRowsError(err) || isNotFoundError(err) {
			writeErrorResponse(w, http.StatusNotFound, "alert not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to resolve alert", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "alert resolved", nil)
}

// ListChannels handles GET /api/alerts/channels - list notification channels.
// Sensitive fields in config_json (webhook_url, bot_token) are masked per requirement 2.8.
func (h *AlertHandler) ListChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.alertService.ListChannels(r.Context())
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to list channels", err.Error())
		return
	}

	if channels == nil {
		channels = []*model.NotificationChannel{}
	}

	// Mask sensitive fields in config_json before returning
	maskedChannels := make([]*model.NotificationChannel, len(channels))
	for i, ch := range channels {
		maskedChannels[i] = maskNotificationChannel(ch)
	}

	writeSuccessResponse(w, http.StatusOK, "success", maskedChannels)
}

// CreateChannel handles POST /api/alerts/channels - create a notification channel.
func (h *AlertHandler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Type       string `json:"type"`
		Name       string `json:"name"`
		ConfigJSON string `json:"config_json"`
		Enabled    *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	// Validate required fields
	if input.Type == "" {
		writeErrorResponse(w, http.StatusBadRequest, "channel type is required", "")
		return
	}
	if input.Type != "lark" && input.Type != "telegram" {
		writeErrorResponse(w, http.StatusBadRequest, "channel type must be 'lark' or 'telegram'", "")
		return
	}
	if input.Name == "" {
		writeErrorResponse(w, http.StatusBadRequest, "channel name is required", "")
		return
	}

	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}

	ch := &model.NotificationChannel{
		Type:       input.Type,
		Name:       input.Name,
		ConfigJSON: input.ConfigJSON,
		Enabled:    enabled,
	}

	if err := h.alertService.CreateChannel(r.Context(), ch); err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to create channel", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusCreated, "channel created", maskNotificationChannel(ch))
}

// UpdateChannel handles PUT /api/alerts/channels/{id} - update a notification channel.
func (h *AlertHandler) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "channel id is required", "")
		return
	}

	var input struct {
		Name       *string `json:"name,omitempty"`
		ConfigJSON *string `json:"config_json,omitempty"`
		Enabled    *bool   `json:"enabled,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	updates := make(map[string]interface{})
	if input.Name != nil {
		updates["name"] = *input.Name
	}
	if input.ConfigJSON != nil {
		// If config_json is empty or contains masked values (***), preserve existing config
		configVal := strings.TrimSpace(*input.ConfigJSON)
		if configVal != "" && !strings.Contains(configVal, "***") {
			updates["config_json"] = *input.ConfigJSON
		}
	}
	if input.Enabled != nil {
		if *input.Enabled {
			updates["enabled"] = 1
		} else {
			updates["enabled"] = 0
		}
	}

	if len(updates) == 0 {
		writeErrorResponse(w, http.StatusBadRequest, "no fields to update", "")
		return
	}

	err := h.alertService.UpdateChannel(r.Context(), id, updates)
	if err != nil {
		if err == sql.ErrNoRows || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "channel not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to update channel", err.Error())
		return
	}

	// Fetch updated channel to return
	ch, err := h.alertService.GetChannel(r.Context(), id)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to get updated channel", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "channel updated", maskNotificationChannel(ch))
}

// DeleteChannel handles DELETE /api/alerts/channels/{id} - delete a notification channel.
func (h *AlertHandler) DeleteChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "channel id is required", "")
		return
	}

	err := h.alertService.DeleteChannel(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows || isNoRowsError(err) {
			writeErrorResponse(w, http.StatusNotFound, "channel not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to delete channel", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "channel deleted", nil)
}

// TestChannel handles POST /api/alerts/channels/{id}/test - test send to a channel.
func (h *AlertHandler) TestChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "channel id is required", "")
		return
	}

	err := h.alertService.TestSend(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows || isNoRowsError(err) || isNotFoundError(err) {
			writeErrorResponse(w, http.StatusNotFound, "channel not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to send test message", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "test message sent", nil)
}

// maskNotificationChannel returns a copy of the channel with sensitive fields in config_json masked.
// Sensitive fields: webhook_url (Lark), bot_token (Telegram).
// Per requirement 2.8: Token, Webhook URL, and password fields must be masked in all config-returning interfaces.
func maskNotificationChannel(ch *model.NotificationChannel) *model.NotificationChannel {
	if ch == nil {
		return nil
	}

	masked := *ch

	// Parse config_json and mask sensitive fields
	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(ch.ConfigJSON), &configMap); err != nil {
		// If we can't parse it, mask the entire config_json
		masked.ConfigJSON = "{}"
		return &masked
	}

	// Mask known sensitive fields
	sensitiveKeys := []string{"webhook_url", "bot_token", "token", "password", "secret"}
	for _, key := range sensitiveKeys {
		if val, ok := configMap[key]; ok {
			if strVal, isStr := val.(string); isStr && strVal != "" {
				configMap[key] = maskString(strVal)
			}
		}
	}

	maskedJSON, err := json.Marshal(configMap)
	if err != nil {
		masked.ConfigJSON = "{}"
		return &masked
	}

	masked.ConfigJSON = string(maskedJSON)
	return &masked
}

// maskString masks a sensitive string, showing only the first 4 and last 4 characters.
// If the string is too short (≤ 10 chars), it's fully masked.
func maskString(s string) string {
	if len(s) <= 10 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:]
}
