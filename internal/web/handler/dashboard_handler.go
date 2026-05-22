package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// DashboardHandler handles HTTP requests for the dashboard.
type DashboardHandler struct {
	dashboardService *service.DashboardService
}

// NewDashboardHandler creates a new DashboardHandler.
func NewDashboardHandler(dashboardService *service.DashboardService) *DashboardHandler {
	return &DashboardHandler{
		dashboardService: dashboardService,
	}
}

// RegisterRoutes registers dashboard routes on the given chi router.
// All routes require authentication.
func (h *DashboardHandler) RegisterRoutes(r chi.Router, authService middleware.AuthService, auditRepo middleware.AuditRepository) {
	r.Route("/api/dashboard", func(r chi.Router) {
		r.Use(middleware.AuthMiddleware(authService))
		r.Use(middleware.AuditMiddleware(auditRepo))
		r.Get("/", h.GetDashboard)
	})
}

// GetDashboard handles GET /api/dashboard
// Returns aggregated statistics for the dashboard overview.
func (h *DashboardHandler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := h.dashboardService.GetStats(r.Context())
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to get dashboard stats", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "success", stats)
}
