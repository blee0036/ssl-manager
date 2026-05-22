package middleware

import (
	"net/http"
)

// RoleMiddleware checks if the authenticated user has one of the required roles.
// Returns 403 if user's role is not in the allowed list.
// Must be used after AuthMiddleware.
func RoleMiddleware(roles ...string) func(http.Handler) http.Handler {
	roleSet := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		roleSet[r] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetUserClaims(r.Context())
			if claims == nil {
				writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
					"code":    401,
					"message": "authentication required",
				})
				return
			}

			if _, ok := roleSet[claims.Role]; !ok {
				writeJSON(w, http.StatusForbidden, map[string]interface{}{
					"code":    403,
					"message": "insufficient permissions",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
