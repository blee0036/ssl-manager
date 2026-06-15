package middleware

import (
	"net/http"
	"strings"
)

// BodyLimiter creates a request body size limiting middleware.
// Two-phase approach:
//
//	Phase 1: Check Content-Length header — if known and exceeds limit, return 413 immediately.
//	Phase 2: Wrap r.Body with http.MaxBytesReader — handles chunked/unknown length requests.
//
// The limits map keys are URL path prefixes (or exact paths) to match against.
func BodyLimiter(limits map[string]int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit, hasLimit := matchLimit(r.URL.Path, limits)
			if !hasLimit {
				next.ServeHTTP(w, r)
				return
			}

			// Phase 1: Content-Length is known and exceeds limit → immediate 413
			if r.ContentLength > limit {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
					"code":    413,
					"message": "request entity too large",
				})
				return
			}

			// Phase 2: Wrap body with MaxBytesReader (handles chunked or untrusted Content-Length)
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

// matchLimit finds the applicable limit for a given request path.
// It tries exact match first, then prefix match (longest prefix wins).
func matchLimit(path string, limits map[string]int64) (int64, bool) {
	// Exact match first
	if limit, ok := limits[path]; ok {
		return limit, true
	}

	// Prefix match — find longest matching prefix
	var bestLimit int64
	var found bool
	for prefix, limit := range limits {
		if strings.HasPrefix(path, prefix) {
			if !found || len(prefix) > len(path) {
				bestLimit = limit
				found = true
			}
		}
	}

	return bestLimit, found
}
