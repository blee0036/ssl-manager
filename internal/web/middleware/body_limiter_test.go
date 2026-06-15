package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func Test_BodyLimiter_ContentLength_Exceeds(t *testing.T) {
	limits := map[string]int64{
		"/api/agent/heartbeat": 65536, // 64 KB
	}

	handler := BodyLimiter(limits)(okHandler())

	// Create a request with Content-Length exceeding the limit
	body := strings.NewReader("x")
	req := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", body)
	req.ContentLength = 100000 // exceeds 65536

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", rec.Code)
	}

	// Verify JSON response body
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["code"] != float64(413) {
		t.Errorf("expected code 413, got %v", resp["code"])
	}
	if resp["message"] != "request entity too large" {
		t.Errorf("expected message 'request entity too large', got %v", resp["message"])
	}
}

func Test_BodyLimiter_Chunked_Exceeds(t *testing.T) {
	limits := map[string]int64{
		"/api/agent/heartbeat": 64, // small limit for testing
	}

	// Next handler reads the body — should get MaxBytesError
	var readErr error
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		if readErr != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(readErr, &maxBytesErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
					"code":    413,
					"message": "request entity too large",
				})
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := BodyLimiter(limits)(nextHandler)

	// Create a request without Content-Length (simulate chunked) but body exceeds limit
	largeBody := bytes.NewReader(bytes.Repeat([]byte("A"), 200)) // 200 bytes > 64 limit
	req := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", largeBody)
	req.ContentLength = -1 // unknown length (chunked)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify that reading the body produced a MaxBytesError
	if readErr == nil {
		t.Fatal("expected an error reading body, got nil")
	}
	var maxBytesErr *http.MaxBytesError
	if !errors.As(readErr, &maxBytesErr) {
		t.Errorf("expected *http.MaxBytesError, got %T: %v", readErr, readErr)
	}

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", rec.Code)
	}
}

func Test_BodyLimiter_NormalSize_Passes(t *testing.T) {
	limits := map[string]int64{
		"/api/agent/deployment-logs": 1048576, // 1 MB
	}

	bodyContent := "hello world"
	var readBody string
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("unexpected error reading body: %v", err)
		}
		readBody = string(data)
		w.WriteHeader(http.StatusOK)
	})

	handler := BodyLimiter(limits)(nextHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/deployment-logs", strings.NewReader(bodyContent))
	req.ContentLength = int64(len(bodyContent))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if readBody != bodyContent {
		t.Errorf("expected body '%s', got '%s'", bodyContent, readBody)
	}
}

func Test_BodyLimiter_NoMatchingPath(t *testing.T) {
	limits := map[string]int64{
		"/api/agent/heartbeat":       65536,
		"/api/agent/deployment-logs": 1048576,
	}

	handlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := BodyLimiter(limits)(nextHandler)

	// Use a path that doesn't match any limit
	largeBody := bytes.NewReader(bytes.Repeat([]byte("X"), 2000000)) // 2 MB
	req := httptest.NewRequest(http.MethodPost, "/api/certificates", largeBody)
	req.ContentLength = 2000000

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !handlerCalled {
		t.Error("expected next handler to be called")
	}
}

func Test_BodyLimiter_ExactLimit(t *testing.T) {
	const limit int64 = 128
	limits := map[string]int64{
		"/api/agent/heartbeat": limit,
	}

	var readBody []byte
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("unexpected error reading body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		readBody = data
		w.WriteHeader(http.StatusOK)
	})

	handler := BodyLimiter(limits)(nextHandler)

	// Body exactly at the limit
	exactBody := bytes.Repeat([]byte("B"), int(limit))
	req := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", bytes.NewReader(exactBody))
	req.ContentLength = limit

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if len(readBody) != int(limit) {
		t.Errorf("expected body length %d, got %d", limit, len(readBody))
	}
}
