package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestProperty10_BodyLimiterRejectsOversizedRequests verifies the two-phase body
// limiting behavior:
// Phase 1: If Content-Length is set and exceeds the limit, return 413 immediately.
// Phase 2: If Content-Length is unknown (-1), MaxBytesReader triggers on read, and
//          the handler detects the error.
//
// **Validates: Requirements 3.1, 3.12**
func TestProperty10_BodyLimiterRejectsOversizedRequests(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Test both configured endpoints
	type endpointCase struct {
		path  string
		limit int64
	}
	endpoints := []endpointCase{
		{path: "/api/agent/deployment-logs", limit: 1 << 20}, // 1 MB
		{path: "/api/agent/heartbeat", limit: 1 << 16},       // 64 KB
	}

	limits := map[string]int64{
		"/api/agent/deployment-logs": 1 << 20,
		"/api/agent/heartbeat":      1 << 16,
	}

	// Phase 1: Content-Length exceeds limit → immediate 413 without reading body
	properties.Property("phase1: Content-Length exceeding limit returns 413 immediately", prop.ForAll(
		func(endpointIdx int, extraBytes int) bool {
			ep := endpoints[endpointIdx]
			// bodySize exceeds limit by extraBytes (at least 1 byte over)
			bodySize := ep.limit + int64(extraBytes)

			handler := BodyLimiter(limits)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// If we reach here, Phase 1 did NOT block — that's a failure
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("POST", ep.path, bytes.NewReader(make([]byte, 0)))
			req.ContentLength = bodySize
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			// Must return 413
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Logf("Expected 413 for Content-Length %d on %s (limit %d), got %d",
					bodySize, ep.path, ep.limit, rec.Code)
				return false
			}

			// Verify JSON response format
			var resp map[string]interface{}
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Logf("Failed to decode 413 JSON response: %v", err)
				return false
			}
			if resp["code"] != float64(413) {
				t.Logf("Expected code 413, got %v", resp["code"])
				return false
			}
			if resp["message"] != "request entity too large" {
				t.Logf("Expected message 'request entity too large', got %v", resp["message"])
				return false
			}

			return true
		},
		gen.IntRange(0, 1), // endpoint index
		gen.IntRange(1, 1024*1024), // extra bytes over limit (1 to 1MB extra)
	))

	// Phase 2: Unknown Content-Length (-1) with oversized body → MaxBytesReader triggers error on read
	properties.Property("phase2: unknown Content-Length with oversized body triggers MaxBytesReader error", prop.ForAll(
		func(endpointIdx int, extraBytes int) bool {
			ep := endpoints[endpointIdx]
			// Create a body that exceeds the limit
			bodySize := int(ep.limit) + extraBytes

			var readErr error
			handler := BodyLimiter(limits)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Try to read the entire body — should fail with MaxBytesError
				_, readErr = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusOK)
			}))

			bigBody := bytes.Repeat([]byte("x"), bodySize)
			req := httptest.NewRequest("POST", ep.path, bytes.NewReader(bigBody))
			req.ContentLength = -1 // unknown content length (simulates chunked)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			// The body read must have produced an error
			if readErr == nil {
				t.Logf("Expected MaxBytesReader error for body size %d on %s (limit %d), got nil",
					bodySize, ep.path, ep.limit)
				return false
			}

			return true
		},
		gen.IntRange(0, 1), // endpoint index
		gen.IntRange(1, 1024), // extra bytes over limit (1 to 1KB extra — enough to trigger)
	))

	// Phase 3 (positive case): Body within limit passes through successfully
	properties.Property("body within limit passes through successfully", prop.ForAll(
		func(endpointIdx int, bodyFraction int) bool {
			ep := endpoints[endpointIdx]
			// bodySize is a fraction of the limit (0% to 100%)
			bodySize := int(ep.limit) * bodyFraction / 100
			if bodySize == 0 {
				bodySize = 1 // at least 1 byte
			}

			var readBody []byte
			var readErr error
			handler := BodyLimiter(limits)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				readBody, readErr = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusOK)
			}))

			body := bytes.Repeat([]byte("y"), bodySize)
			req := httptest.NewRequest("POST", ep.path, bytes.NewReader(body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			// Request should pass through
			if rec.Code != http.StatusOK {
				t.Logf("Expected 200 for body size %d on %s (limit %d), got %d",
					bodySize, ep.path, ep.limit, rec.Code)
				return false
			}

			// Body should be fully readable without error
			if readErr != nil {
				t.Logf("Unexpected body read error for size %d: %v", bodySize, readErr)
				return false
			}

			if len(readBody) != bodySize {
				t.Logf("Body size mismatch: expected %d, got %d", bodySize, len(readBody))
				return false
			}

			return true
		},
		gen.IntRange(0, 1),    // endpoint index
		gen.IntRange(1, 100),  // body fraction percentage (1% to 100% of limit)
	))

	properties.TestingRun(t)
}
