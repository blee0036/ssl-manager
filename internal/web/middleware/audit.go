package middleware

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
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
			if path == "/api/auth/login" || path == "/api/auth/logout" || path == "/api/auth/readonly-login" {
				next.ServeHTTP(w, r)
				return
			}

			// Wrap the response writer to capture status code
			capture := newResponseCapture(w)

			// Place an AuditInfo pointer in context so handlers can populate it
			ctx := withAuditInfoPlaceholder(r.Context())
			r = r.WithContext(ctx)

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

				// Extract target info from path (handles nested resources)
				targetType, targetID := extractTargetFromPath(path)

				// Check if handler provided explicit audit info (overrides URL-guessed values)
				if info := getAuditInfo(r.Context()); info != nil {
					if info.TargetType != "" {
						targetType = info.TargetType
					}
					if info.TargetID != "" {
						targetID = info.TargetID
					}
				}

				// Determine operation: prefer handler-provided, then derive from method+path
				operation := ""
				if info := getAuditInfo(r.Context()); info != nil && info.Operation != "" {
					operation = info.Operation
				}

				// Build structured detail with operation context
				detail := buildAuditDetail(method, path, capturedStatus, claims, operation)

				// Sanitize detail to remove any sensitive data before persisting
				detail = sanitizeAuditDetail(detail)

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
// Handles nested resources like /api/machines/{id}/certificates/{mc_id}/deploy
func extractTargetFromPath(path string) (targetType, targetID string) {
	// Remove /api/ prefix
	path = strings.TrimPrefix(path, "/api/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 {
		return "", ""
	}

	// Handle nested resources: prefer the deepest resource
	// /machines/{id}/certificates/{mc_id}/deployment-logs → machine_certificate, mc_id
	// /machines/{id}/certificates/{mc_id}/deploy → machine_certificate, mc_id
	// /machines/{id}/certificates → machine_certificate, ""
	// /machines/{id}/regenerate-token → machine, id
	// /thirdpart-dns/{id}/sync → thirdpart_dns, id
	// /alerts/channels/{id} → alert_channel, id

	if len(parts) >= 4 {
		// Nested resource: parent/{parentID}/child/{childID}
		parentResource := parts[0]
		childResource := parts[2]

		// Special cases for nested resources
		if parentResource == "machines" && childResource == "certificates" {
			targetType = "machine_certificate"
			if len(parts) >= 4 {
				targetID = parts[3]
			}
			return
		}
		if parentResource == "alerts" && childResource == "channels" {
			targetType = "alert_channel"
			// alerts/channels/{id} — parts[0]="alerts", parts[1]="channels", parts[2]={id}
			// Actually this is /api/alerts/channels/{id} → parts = ["alerts", "channels", "{id}"]
		}
	}

	// Handle /machines/{id}/certificates pattern (3 parts: machines, {id}, certificates)
	// This is the create deployment config endpoint: POST /api/machines/{machine_id}/certificates
	if len(parts) == 3 && parts[0] == "machines" && parts[2] == "certificates" {
		targetType = "machine_certificate"
		targetID = "" // new resource ID not yet known from URL; handler should set via SetAuditInfo
		return
	}

	// Handle /agent/deployment-logs pattern
	if len(parts) >= 2 && parts[0] == "agent" && parts[1] == "deployment-logs" {
		targetType = "deployment_log"
		targetID = ""
		return
	}

	// Handle /alerts/channels/{id} pattern (3 parts)
	if len(parts) >= 2 && parts[0] == "alerts" && parts[1] == "channels" {
		targetType = "alert_channel"
		if len(parts) >= 3 {
			targetID = parts[2]
		}
		return
	}

	// Default: first segment is resource type, second is ID
	targetType = singularize(parts[0])
	if len(parts) >= 2 {
		// Skip action segments like "regenerate-token", "revoke-token", "sync", "deploy"
		candidate := parts[1]
		if !isActionSegment(candidate) {
			targetID = candidate
		} else if len(parts) == 2 {
			// e.g., /api/system/config → target is "system", id is ""
			targetID = ""
		}
	}

	return targetType, targetID
}

// isActionSegment returns true if the path segment is an action verb, not an ID.
func isActionSegment(s string) bool {
	actions := map[string]bool{
		"regenerate-token": true,
		"revoke-token":     true,
		"sync":             true,
		"deploy":           true,
		"disable":          true,
		"reset-password":   true,
		"test":             true,
		"probe":            true,
		"config":           true,
		"upload":           true,
		"issue":            true,
	}
	return actions[s]
}

// buildAuditDetail constructs a structured detail string for the audit log.
// Includes: operation description, response status, and actor context.
// If operationOverride is non-empty, it is used instead of deriving from method+path.
func buildAuditDetail(method, path string, status int, claims *TokenClaims, operationOverride string) string {
	operation := operationOverride
	if operation == "" {
		operation = describeOperation(method, path)
	}
	result := "success"
	if status >= 400 {
		result = fmt.Sprintf("failed(HTTP %d)", status)
	}

	detail := fmt.Sprintf(`{"operation":"%s","result":"%s","status":%d`, operation, result, status)
	if claims != nil {
		detail += fmt.Sprintf(`,"actor_username":"%s","actor_role":"%s"`, claims.Username, claims.Role)
	}
	detail += "}"
	return detail
}

// describeOperation returns a human-readable operation description from method + path.
func describeOperation(method, path string) string {
	cleanPath := strings.TrimPrefix(path, "/api/")
	parts := strings.Split(cleanPath, "/")

	if len(parts) == 0 {
		return method
	}

	resource := parts[0]

	switch method {
	case "POST":
		// Check for action sub-paths
		if len(parts) >= 3 {
			action := parts[len(parts)-1]
			switch action {
			case "regenerate-token":
				return "regenerate_token"
			case "revoke-token":
				return "revoke_token"
			case "deploy":
				return "trigger_deploy"
			case "sync":
				return "trigger_sync"
			case "disable":
				return "disable_user"
			case "reset-password":
				return "reset_password"
			case "test":
				return "test_send"
			case "probe":
				return "probe_domain"
			}
		}
		if len(parts) >= 2 {
			action := parts[len(parts)-1]
			switch action {
			case "disable":
				return "disable_user"
			case "reset-password":
				return "reset_password"
			case "test":
				return "test_send"
			case "probe":
				return "probe_domain"
			case "sync":
				return "trigger_sync"
			}
		}
		return "create_" + singularize(resource)
	case "PUT":
		return "update_" + singularize(resource)
	case "DELETE":
		return "delete_" + singularize(resource)
	default:
		return method + "_" + singularize(resource)
	}
}

// sanitizeAuditDetail applies sanitization to remove sensitive data from audit detail.
// Redacts: private keys, tokens, webhook URLs, passwords, secrets.
// This ensures sensitive data is never persisted in audit logs regardless of the write path.
func sanitizeAuditDetail(detail string) string {
	if detail == "" {
		return detail
	}
	// Apply regex-based sanitization for common sensitive patterns
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?s)-----BEGIN\s+(RSA\s+)?PRIVATE\s+KEY-----.*?-----END\s+(RSA\s+)?PRIVATE\s+KEY-----`),
		regexp.MustCompile(`(?i)(token|agent_token|api_token|secret|password|passwd)["\s:=]+["\s]*([A-Za-z0-9+/=_\-]{16,})`),
		regexp.MustCompile(`(?i)https?://[^\s"',}]*(?:webhook|hooks?\.slack\.com|hook/)[^\s"',}]*`),
		regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9+/=_\-\.]{10,}`),
	}
	result := detail
	for _, p := range patterns {
		result = p.ReplaceAllString(result, "[REDACTED]")
	}
	return result
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
