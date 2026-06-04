package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// UserHandler handles HTTP requests for user management.
type UserHandler struct {
	userService *service.UserService
}

// NewUserHandler creates a new UserHandler.
func NewUserHandler(userService *service.UserService) *UserHandler {
	return &UserHandler{
		userService: userService,
	}
}

// RegisterRoutes registers user management routes on the given chi router.
// All routes require authentication and admin role.
func (h *UserHandler) RegisterRoutes(r chi.Router, authService middleware.AuthService, auditRepo middleware.AuditRepository) {
	r.Route("/api/users", func(r chi.Router) {
		r.Use(middleware.AuthMiddleware(authService))
		r.Use(middleware.AuditMiddleware(auditRepo))
		r.Use(middleware.RoleMiddleware("admin"))

		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Put("/{id}", h.UpdateRole)
		r.Post("/{id}/disable", h.Disable)
		r.Post("/{id}/reset-password", h.ResetPassword)
	})
}

// List handles GET /api/users/ — list all users.
func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	users, err := h.userService.List(r.Context())
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to list users", err.Error())
		return
	}

	if users == nil {
		writeSuccessResponse(w, http.StatusOK, "success", []interface{}{})
		return
	}

	// Strip password hashes from response
	type userResponse struct {
		ID        string `json:"id"`
		Username  string `json:"username"`
		Role      string `json:"role"`
		Enabled   bool   `json:"enabled"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}

	var resp []userResponse
	for _, u := range users {
		resp = append(resp, userResponse{
			ID:        u.ID,
			Username:  u.Username,
			Role:      u.Role,
			Enabled:   u.Enabled,
			CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt: u.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	writeSuccessResponse(w, http.StatusOK, "success", resp)
}

// Create handles POST /api/users/ — create a new user.
func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	var input service.CreateUserInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	user, err := h.userService.Create(r.Context(), input)
	if err != nil {
		if isValidationError(err) || isAlreadyExistsError(err) {
			writeErrorResponse(w, http.StatusBadRequest, err.Error(), "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to create user", err.Error())
		return
	}

	// Set audit info with the newly created user ID
	middleware.SetAuditInfo(r, middleware.AuditInfo{
		TargetType: "user",
		TargetID:   user.ID,
		Operation:  "create_user",
	})

	writeSuccessResponse(w, http.StatusCreated, "user created", map[string]interface{}{
		"id":       user.ID,
		"username": user.Username,
		"role":     user.Role,
		"enabled":  user.Enabled,
	})
}

// UpdateRole handles PUT /api/users/{id} — update a user's role.
func (h *UserHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "user id is required", "")
		return
	}

	var input service.UpdateUserRoleInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	user, err := h.userService.UpdateRole(r.Context(), id, input)
	if err != nil {
		if isNotFoundError(err) {
			writeErrorResponse(w, http.StatusNotFound, "user not found", "")
			return
		}
		if isValidationError(err) {
			writeErrorResponse(w, http.StatusBadRequest, err.Error(), "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to update user", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "user role updated", map[string]interface{}{
		"id":       user.ID,
		"username": user.Username,
		"role":     user.Role,
		"enabled":  user.Enabled,
	})
}

// Disable handles POST /api/users/{id}/disable — disable a user.
func (h *UserHandler) Disable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "user id is required", "")
		return
	}

	// Prevent admin from disabling themselves
	claims := middleware.GetUserClaims(r.Context())
	if claims != nil && claims.UserID == id {
		writeErrorResponse(w, http.StatusBadRequest, "cannot disable your own account", "")
		return
	}

	err := h.userService.Disable(r.Context(), id)
	if err != nil {
		if isNotFoundError(err) {
			writeErrorResponse(w, http.StatusNotFound, "user not found", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to disable user", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "user disabled", nil)
}

// ResetPassword handles POST /api/users/{id}/reset-password — reset a user's password.
func (h *UserHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "user id is required", "")
		return
	}

	var input service.ResetPasswordInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	err := h.userService.ResetPassword(r.Context(), id, input)
	if err != nil {
		if isNotFoundError(err) {
			writeErrorResponse(w, http.StatusNotFound, "user not found", "")
			return
		}
		if isValidationError(err) {
			writeErrorResponse(w, http.StatusBadRequest, err.Error(), "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to reset password", err.Error())
		return
	}

	writeSuccessResponse(w, http.StatusOK, "password reset successful", nil)
}

// isAlreadyExistsError checks if an error indicates a duplicate/already exists condition.
func isAlreadyExistsError(err error) bool {
	return err != nil && (err.Error() == "username already exists")
}
