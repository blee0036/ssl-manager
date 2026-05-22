package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// DeploymentLogHandler handles HTTP requests for deployment log queries.
type DeploymentLogHandler struct {
	logService *service.DeploymentLogService
}

// NewDeploymentLogHandler creates a new DeploymentLogHandler.
func NewDeploymentLogHandler(logService *service.DeploymentLogService) *DeploymentLogHandler {
	return &DeploymentLogHandler{
		logService: logService,
	}
}

// RegisterRoutes registers deployment log routes on the given chi router.
// Routes are nested under /api/machines/{machine_id}/deployment-logs.
// All routes require authentication.
func (h *DeploymentLogHandler) RegisterRoutes(r chi.Router, authService middleware.AuthService, auditRepo middleware.AuditRepository) {
	r.Route("/api/machines/{machine_id}/deployment-logs", func(r chi.Router) {
		r.Use(middleware.AuthMiddleware(authService))
		r.Use(middleware.AuditMiddleware(auditRepo))

		// GET /api/machines/{machine_id}/deployment-logs
		r.Get("/", h.ListByMachine)
	})

	r.Route("/api/machines/{machine_id}/certificates/{mc_id}/deployment-logs", func(r chi.Router) {
		r.Use(middleware.AuthMiddleware(authService))
		r.Use(middleware.AuditMiddleware(auditRepo))

		// GET /api/machines/{machine_id}/certificates/{mc_id}/deployment-logs
		r.Get("/", h.ListByMachineCertificate)
	})
}

// ListByMachine handles GET /api/machines/{machine_id}/deployment-logs
// Returns deployment logs for the specified machine in time DESC order.
func (h *DeploymentLogHandler) ListByMachine(w http.ResponseWriter, r *http.Request) {
	machineID := chi.URLParam(r, "machine_id")
	if machineID == "" {
		writeErrorResponse(w, http.StatusBadRequest, "machine_id is required", "")
		return
	}

	logs, err := h.logService.GetByMachineID(r.Context(), machineID)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to list deployment logs", err.Error())
		return
	}

	if logs == nil {
		logs = []*model.DeploymentLog{}
	}

	writeSuccessResponse(w, http.StatusOK, "success", logs)
}

// ListByMachineCertificate handles GET /api/machines/{machine_id}/certificates/{mc_id}/deployment-logs
// Returns deployment logs for a specific machine certificate in time DESC order.
func (h *DeploymentLogHandler) ListByMachineCertificate(w http.ResponseWriter, r *http.Request) {
	mcID := chi.URLParam(r, "mc_id")
	if mcID == "" {
		writeErrorResponse(w, http.StatusBadRequest, "machine certificate id is required", "")
		return
	}

	logs, err := h.logService.GetByMachineCertificateID(r.Context(), mcID)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to list deployment logs", err.Error())
		return
	}

	if logs == nil {
		logs = []*model.DeploymentLog{}
	}

	writeSuccessResponse(w, http.StatusOK, "success", logs)
}
