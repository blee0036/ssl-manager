package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

// AuditRepository defines the interface for audit log persistence.
// Using an interface avoids circular dependency with the repository package.
type AuditRepository interface {
	CreateAuditLog(ctx context.Context, log *model.AuditLog) error
}

// responseCapture wraps http.ResponseWriter to capture the response status code.
type responseCapture struct {
	http.ResponseWriter
	statusCode int
}

func newResponseCapture(w http.ResponseWriter) *responseCapture {
	return &responseCapture{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rc *responseCapture) WriteHeader(code int) {
	rc.statusCode = code
	rc.ResponseWriter.WriteHeader(code)
}

// AuditMiddleware logs write operations (POST/PUT/DELETE) to audit_logs.
// Captures: actor_type, actor_id, action (HTTP method + path), target info, IP, and response status.
// Must be used after AuthMiddleware (to get user claims from context).
func AuditMiddleware(auditRepo AuditRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only audit write operations
			method := r.Method
			if method != http.MethodPost && method != http.MethodPut && method != http.MethodDelete {
				next.ServeHTTP(w, r)
				return
			}

			// Skip auth endpoints from audit (login/logout are not sensitive write ops)
			path := r.URL.Path
			if path == "/api/auth/login" || path == "/api/auth/logout" {
				next.ServeHTTP(w, r)
				return
			}

			// Wrap the response writer to capture status code
			capture := newResponseCapture(w)

			// Serve the request
			next.ServeHTTP(capture, r)

			// Record audit log asynchronously to not block the response
			capturedStatus := capture.statusCode
			go func() {
				actorType := "user"
				actorID := ""

				claims := GetUserClaims(r.Context())
				if claims != nil {
					actorID = claims.UserID
					if claims.Role == "readonly" {
						actorType = "user"
					}
				}

				// If no user claims, check for machine (agent)
				machine := GetMachine(r.Context())
				if machine != nil {
					actorType = "agent"
					actorID = machine.ID
				}

				// Extract target info from path
				targetType, targetID := extractTargetFromPath(path)

				// Include response status in detail field
				detail := fmt.Sprintf("status:%d", capturedStatus)

				auditLog := &model.AuditLog{
					ID:         uuid.New().String(),
					ActorType:  actorType,
					ActorID:    actorID,
					Action:     method + " " + path,
					TargetType: targetType,
					TargetID:   targetID,
					Detail:     detail,
					IP:         extractClientIP(r),
					CreatedAt:  time.Now().UTC(),
				}

				// Best-effort logging; don't fail the request if audit fails
				_ = auditRepo.CreateAuditLog(context.Background(), auditLog)
			}()
		})
	}
}

// extractClientIP extracts the client IP from the request.
func extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fall back to RemoteAddr
	addr := r.RemoteAddr
	// Strip port if present
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

// extractTargetFromPath extracts target type and ID from the URL path.
// Example: /api/certificates/abc-123 -> ("certificate", "abc-123")
func extractTargetFromPath(path string) (targetType, targetID string) {
	// Remove /api/ prefix
	path = strings.TrimPrefix(path, "/api/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 {
		return "", ""
	}

	// The first segment is typically the resource type (plural)
	targetType = singularize(parts[0])

	// The second segment (if exists) is typically the ID
	if len(parts) >= 2 {
		targetID = parts[1]
	}

	return targetType, targetID
}

// singularize converts a plural resource name to singular.
func singularize(s string) string {
	switch s {
	case "certificates":
		return "certificate"
	case "machines":
		return "machine"
	case "users":
		return "user"
	case "domains":
		return "domain"
	case "alerts":
		return "alert"
	case "thirdpart-dns":
		return "thirdpart_dns"
	case "audit-logs":
		return "audit_log"
	case "agent":
		return "agent"
	case "system":
		return "system"
	default:
		return s
	}
}
