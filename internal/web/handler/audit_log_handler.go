package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// AuditLogHandler handles HTTP requests for audit log queries.
type AuditLogHandler struct {
	auditLogService *service.AuditLogService
}

// NewAuditLogHandler creates a new AuditLogHandler.
func NewAuditLogHandler(auditLogService *service.AuditLogService) *AuditLogHandler {
	return &AuditLogHandler{
		auditLogService: auditLogService,
	}
}

// RegisterRoutes registers audit log routes on the given chi router.
// All routes require authentication. Audit logs are viewable by all authenticated users.
func (h *AuditLogHandler) RegisterRoutes(r chi.Router, authService middleware.AuthService, auditRepo middleware.AuditRepository) {
	r.Route("/api/audit-logs", func(r chi.Router) {
		r.Use(middleware.AuthMiddleware(authService))
		r.Use(middleware.AuditMiddleware(auditRepo))
		r.Use(middleware.ReadonlyMiddleware())

		// GET /api/audit-logs - list audit logs (all authenticated users)
		r.Get("/", h.ListAuditLogs)
	})
}

// ListAuditLogs handles GET /api/audit-logs
// Returns audit logs in time DESC order with optional filters.
// Query params: actor_type, target_type, limit, offset
// Accessible by all authenticated users (admin, user, readonly).
func (h *AuditLogHandler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {

	// Parse query parameters
	filter := repository.AuditLogFilter{
		ActorType:  r.URL.Query().Get("actor_type"),
		TargetType: r.URL.Query().Get("target_type"),
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			filter.Limit = limit
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			filter.Offset = offset
		}
	}

	logs, err := h.auditLogService.List(r.Context(), filter)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to list audit logs", err.Error())
		return
	}

	if logs == nil {
		logs = []*model.AuditLog{}
	}

	// Get total count for pagination
	total, err := h.auditLogService.Count(r.Context(), filter)
	if err != nil {
		// Non-fatal: fall back to length of current page
		total = len(logs)
	}

	writeSuccessResponse(w, http.StatusOK, "success", map[string]interface{}{
		"items": logs,
		"total": total,
	})
}
