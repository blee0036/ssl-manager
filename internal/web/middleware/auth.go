package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AuthService defines the interface needed by the auth middleware.
// This avoids circular dependency with the actual service package.
type AuthService interface {
	// GetJWTSecret returns the JWT signing secret.
	GetJWTSecret() []byte
	// IsSessionValid checks if a session has not been invalidated.
	IsSessionValid(ctx context.Context, sessionID string) bool
	// IsUserActive checks if a user account is still enabled.
	// Returns true if the user is active, false if disabled or not found.
	IsUserActive(ctx context.Context, userID string) bool
	// GetCurrentRole returns the current role for a user from the database.
	// Returns the role string and any error. If user not found, returns empty string and error.
	GetCurrentRole(ctx context.Context, userID string) (string, error)
	// IsTokenValid checks if a token is still valid based on user ID and issued time.
	// Returns false if the token was issued before a session invalidation event (e.g., password reset).
	IsTokenValid(ctx context.Context, userID string, issuedAt time.Time) bool
}

// AuthMiddleware validates JWT token from the Authorization header.
// Sets user claims in request context.
// Returns 401 if token is invalid or session is invalidated.
func AuthMiddleware(authService AuthService) func(http.Handler) http.Handler {
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

			// Expect "Bearer <token>"
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
					"code":    401,
					"message": "invalid authorization header format",
				})
				return
			}

			tokenStr := parts[1]
			secret := authService.GetJWTSecret()

			// Parse and validate the token
			token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return secret, nil
			}, jwt.WithValidMethods([]string{"HS256"}))

			if err != nil || !token.Valid {
				writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
					"code":    401,
					"message": "invalid or expired token",
				})
				return
			}

			// Extract claims
			mapClaims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
					"code":    401,
					"message": "invalid token claims",
				})
				return
			}

			claims := &TokenClaims{}
			if v, ok := mapClaims["user_id"].(string); ok {
				claims.UserID = v
			}
			if v, ok := mapClaims["username"].(string); ok {
				claims.Username = v
			}
			if v, ok := mapClaims["role"].(string); ok {
				claims.Role = v
			}
			if v, ok := mapClaims["session_id"].(string); ok {
				claims.SessionID = v
			}
			if v, ok := mapClaims["iat"].(float64); ok {
				claims.IssuedAt = time.Unix(int64(v), 0)
			}
			if v, ok := mapClaims["exp"].(float64); ok {
				claims.ExpiresAt = time.Unix(int64(v), 0)
			}

			// Check if session is still valid (not invalidated by admin)
			if claims.SessionID != "" && !authService.IsSessionValid(r.Context(), claims.SessionID) {
				writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
					"code":    401,
					"message": "session has been invalidated",
				})
				return
			}

			// Check if user is still active (not disabled)
			if claims.UserID != "" && claims.UserID != "readonly" {
				if !authService.IsUserActive(r.Context(), claims.UserID) {
					writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
						"code":    401,
						"message": "account has been disabled",
					})
					return
				}

				// Override role from database to reflect any role changes
				if dbRole, err := authService.GetCurrentRole(r.Context(), claims.UserID); err == nil && dbRole != "" {
					claims.Role = dbRole
				}

				// Check if token was issued before a session invalidation (e.g., password reset)
				if !authService.IsTokenValid(r.Context(), claims.UserID, claims.IssuedAt) {
					writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
						"code":    401,
						"message": "session invalidated, please login again",
					})
					return
				}
			}

			// Set claims in context
			ctx := context.WithValue(r.Context(), UserClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
