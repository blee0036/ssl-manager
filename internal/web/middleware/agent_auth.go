package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/ssl-manager/ssl-manager/internal/model"
)

// MachineRepository defines the interface needed by the agent auth middleware.
type MachineRepository interface {
	GetByTokenHash(ctx context.Context, tokenHash string) (*model.Machine, error)
	GetByTokenHashIncludingRevoked(ctx context.Context, tokenHash string) (*model.Machine, error)
}

// AgentAlertSender defines the interface for sending alerts from the agent auth middleware.
type AgentAlertSender interface {
	SendAlert(ctx context.Context, level, alertType, title, content, targetType, targetID string) error
	AutoResolve(ctx context.Context, targetType, targetID, alertType string)
}

// AgentAuthMiddleware validates Agent token from the Authorization header.
// Verifies the token hash matches a machine and the machine_id in the URL matches.
// Returns 401 if token is invalid or revoked.
// Triggers an alert when a revoked token is used.
// Sets machine info in request context.
func AgentAuthMiddleware(machineRepo MachineRepository, alertSender AgentAlertSender) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
					"code":    401,
					"message": "missing authorization header",
				})
				return
			}

			// Expect "Bearer <agent-token>"
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
					"code":    401,
					"message": "invalid authorization header format",
				})
				return
			}

			token := parts[1]

			// Hash the token with SHA256 to look up the machine
			hash := sha256.Sum256([]byte(token))
			tokenHash := hex.EncodeToString(hash[:])

			// Look up machine by token hash (non-revoked only)
			machine, err := machineRepo.GetByTokenHash(r.Context(), tokenHash)
			if err != nil || machine == nil {
				// Check if this is a revoked token to trigger alert
				if alertSender != nil {
					revokedMachine, revokedErr := machineRepo.GetByTokenHashIncludingRevoked(r.Context(), tokenHash)
					if revokedErr == nil && revokedMachine != nil && revokedMachine.AgentTokenRevokedAt != nil {
						// This is a revoked token - trigger alert
						alertContent := fmt.Sprintf(
							"Revoked Agent Token used for machine %s (%s, IP: %s). Request from %s to %s.",
							revokedMachine.Name, revokedMachine.ID, revokedMachine.IP,
							r.RemoteAddr, r.URL.Path,
						)
						if alertErr := alertSender.SendAlert(
							r.Context(), "critical", "revoked_token_used",
							"Revoked Agent Token Request Detected",
							alertContent, "machine", revokedMachine.ID,
						); alertErr != nil {
							log.Printf("[AgentAuth] Failed to send revoked token alert: %v", alertErr)
						}
					}
				}

				writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
					"code":    401,
					"message": "invalid or revoked agent token",
				})
				return
			}

			// Verify machine_id in URL matches the machine found by token
			machineIDFromURL := extractMachineIDFromPath(r.URL.Path)
			if machineIDFromURL != "" && machineIDFromURL != machine.ID {
				writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
					"code":    401,
					"message": "token does not match the requested machine",
				})
				return
			}

			// Set machine in context
			ctx := context.WithValue(r.Context(), MachineKey, machine)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractMachineIDFromPath extracts machine_id from Agent API paths.
// Supported patterns:
//   - /api/agent/machines/{machine_id}/certificates
//   - /api/agent/heartbeat (no machine_id in path, uses body)
//   - /api/agent/machine-certificates/{id}/download (no machine_id)
//   - /api/agent/deployment-logs (no machine_id)
func extractMachineIDFromPath(path string) string {
	// Look for /api/agent/machines/{machine_id}/ pattern
	const prefix = "/api/agent/machines/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}

	rest := path[len(prefix):]
	// machine_id is the next segment
	if idx := strings.Index(rest, "/"); idx > 0 {
		return rest[:idx]
	}
	// If no trailing slash, the whole rest is the machine_id
	if rest != "" {
		return rest
	}
	return ""
}

// HashToken computes the SHA256 hash of a token string.
// Exported for use by other packages (e.g., machine service when generating tokens).
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
