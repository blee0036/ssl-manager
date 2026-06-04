package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// staticAssetExtensions lists file extensions that are always considered static assets.
var staticAssetExtensions = []string{
	".js", ".css", ".svg", ".ico", ".png", ".jpg", ".jpeg", ".gif", ".webp",
	".woff", ".woff2", ".ttf", ".eot", ".map", ".json",
}

// isStaticAsset returns true if the path ends with a known static asset extension.
func isStaticAsset(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range staticAssetExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// InitChecker defines the interface for checking initialization status.
type InitChecker interface {
	// NeedsInit returns true if the system needs initialization.
	NeedsInit(ctx context.Context) (bool, error)
	// IsFullyInitialized returns true when both admin is created and config is saved.
	IsFullyInitialized(ctx context.Context) (bool, error)
}

// InitMiddleware redirects all requests to /init when the system is uninitialized.
// Once fully initialized (admin created + config saved), /init/* returns 403.
// Requests to /health are always allowed through.
func InitMiddleware(checker InitChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			// Always allow /health
			if path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			// Always allow static assets
			if strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/favicon") {
				next.ServeHTTP(w, r)
				return
			}

			// Allow common static asset file extensions
			if isStaticAsset(path) {
				next.ServeHTTP(w, r)
				return
			}

			// For /init/* paths: allow only during initialization window
			if strings.HasPrefix(path, "/init") {
				fullyInit, err := checker.IsFullyInitialized(r.Context())
				if err != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    500,
						"message": "failed to check initialization status",
					})
					return
				}
				if fullyInit {
					// Initialization complete — block all /init/* access
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"code":    403,
						"message": "system is already initialized",
					})
					return
				}
				// Still in init window — allow through
				next.ServeHTTP(w, r)
				return
			}

			// Check if system needs initialization
			needsInit, err := checker.NeedsInit(r.Context())
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    500,
					"message": "failed to check initialization status",
				})
				return
			}

			if needsInit {
				// Redirect to /init for browser requests, return JSON for API requests
				accept := r.Header.Get("Accept")
				if strings.Contains(accept, "text/html") {
					http.Redirect(w, r, "/init", http.StatusTemporaryRedirect)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    503,
					"message": "system not initialized",
					"redirect": "/init",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
