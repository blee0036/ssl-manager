package middleware

import (
	"net/http"
	"strings"
)

// readonlyAllowedEndpoints defines the whitelist of endpoints accessible by readonly sessions.
// Format: "METHOD /path" - exact match or prefix match with trailing wildcard.
var readonlyAllowedEndpoints = []routeRule{
	// Auth endpoints (login/logout always allowed)
	{method: "POST", path: "/api/auth/login"},
	{method: "POST", path: "/api/auth/logout"},

	// Read-only GET endpoints
	{method: "GET", path: "/api/certificates", prefix: true},
	{method: "GET", path: "/api/machines", prefix: true},
	{method: "GET", path: "/api/domains", prefix: true},
	{method: "GET", path: "/api/alerts", prefix: true},
	{method: "GET", path: "/api/audit-logs", prefix: true},
	{method: "GET", path: "/api/dashboard", prefix: true},
	{method: "GET", path: "/api/deployment-logs", prefix: true},
	// 注意：/api/system 已移除 — readonly 不可访问系统配置
}

// readonlyBlockedEndpoints defines specific endpoints that are blocked even for GET.
// These are checked before the whitelist.
var readonlyBlockedEndpoints = []routeRule{
	// Private key download (Agent endpoint, but block for readonly)
	{method: "GET", path: "/api/agent/machine-certificates/", suffix: "/download"},
	// Install command contains sensitive info (machine_id, server URL)
	{method: "GET", path: "/api/machines/", suffix: "/install-command"},
}

// routeRule defines a route matching rule.
type routeRule struct {
	method string
	path   string
	prefix bool   // if true, matches path as prefix
	suffix string // if non-empty, path must end with this suffix
}

// ReadonlyMiddleware blocks write operations for readonly sessions.
// Uses an interface whitelist approach - only explicitly allowed endpoints are accessible.
// Readonly sessions are identified by role == "readonly" in the user claims.
// Returns 403 for blocked operations.
// Must be used after AuthMiddleware.
func ReadonlyMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetUserClaims(r.Context())
			if claims == nil {
				// No claims means not authenticated; let auth middleware handle it
				next.ServeHTTP(w, r)
				return
			}

			// Only apply restrictions to readonly role
			if claims.Role != "readonly" {
				next.ServeHTTP(w, r)
				return
			}

			method := r.Method
			path := r.URL.Path

			// Check blocked endpoints first (even if they match whitelist patterns)
			if isBlockedForReadonly(method, path) {
				writeJSON(w, http.StatusForbidden, map[string]interface{}{
					"code":    403,
					"message": "readonly session cannot access this resource",
				})
				return
			}

			// Check if the endpoint is in the whitelist
			if isAllowedForReadonly(method, path) {
				next.ServeHTTP(w, r)
				return
			}

			// Default: block everything not in the whitelist
			writeJSON(w, http.StatusForbidden, map[string]interface{}{
				"code":    403,
				"message": "readonly session cannot perform this operation",
			})
		})
	}
}

// isAllowedForReadonly checks if a request matches the readonly whitelist.
func isAllowedForReadonly(method, path string) bool {
	for _, rule := range readonlyAllowedEndpoints {
		if !strings.EqualFold(rule.method, method) {
			continue
		}
		if rule.prefix {
			if strings.HasPrefix(path, rule.path) {
				return true
			}
		} else {
			if path == rule.path {
				return true
			}
		}
	}
	return false
}

// isBlockedForReadonly checks if a request matches the explicitly blocked list.
func isBlockedForReadonly(method, path string) bool {
	for _, rule := range readonlyBlockedEndpoints {
		if !strings.EqualFold(rule.method, method) {
			continue
		}
		if rule.suffix != "" {
			if strings.HasPrefix(path, rule.path) && strings.HasSuffix(path, rule.suffix) {
				return true
			}
		} else if rule.prefix {
			if strings.HasPrefix(path, rule.path) {
				return true
			}
		} else {
			if path == rule.path {
				return true
			}
		}
	}
	return false
}
