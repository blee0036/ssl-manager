package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockInitChecker implements InitChecker for testing.
type mockInitChecker struct {
	needsInit      bool
	fullyInitialized bool
	err            error
}

func (m *mockInitChecker) NeedsInit(ctx context.Context) (bool, error) {
	return m.needsInit, m.err
}

func (m *mockInitChecker) IsFullyInitialized(ctx context.Context) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.fullyInitialized, nil
}

func TestInitMiddleware_AllowsInitRoutes(t *testing.T) {
	checker := &mockInitChecker{needsInit: true, fullyInitialized: false}
	mw := InitMiddleware(checker)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// /init/status should pass through even when system needs init
	req := httptest.NewRequest(http.MethodGet, "/init/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected /init/status to pass through, got status %d", w.Code)
	}
}

func TestInitMiddleware_AllowsHealthRoute(t *testing.T) {
	checker := &mockInitChecker{needsInit: true, fullyInitialized: false}
	mw := InitMiddleware(checker)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected /health to pass through, got status %d", w.Code)
	}
}

func TestInitMiddleware_RedirectsWhenUninitialized_HTML(t *testing.T) {
	checker := &mockInitChecker{needsInit: true, fullyInitialized: false}
	mw := InitMiddleware(checker)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/machines", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTemporaryRedirect {
		t.Errorf("expected redirect 307, got status %d", w.Code)
	}

	location := w.Header().Get("Location")
	if location != "/init" {
		t.Errorf("expected redirect to /init, got %s", location)
	}
}

func TestInitMiddleware_Returns503WhenUninitialized_API(t *testing.T) {
	checker := &mockInitChecker{needsInit: true, fullyInitialized: false}
	mw := InitMiddleware(checker)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/machines", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestInitMiddleware_PassesThroughWhenInitialized(t *testing.T) {
	checker := &mockInitChecker{needsInit: false, fullyInitialized: true}
	mw := InitMiddleware(checker)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/machines", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestInitMiddleware_BlocksMultiplePathsWhenUninitialized(t *testing.T) {
	checker := &mockInitChecker{needsInit: true, fullyInitialized: false}
	mw := InitMiddleware(checker)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	paths := []string{
		"/api/machines",
		"/api/certificates",
		"/api/users",
		"/api/dashboard",
		"/api/system/config",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Accept", "application/json")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("expected status 503 for path %s, got %d", path, w.Code)
			}
		})
	}
}

func TestInitMiddleware_PassesThroughMultiplePathsWhenInitialized(t *testing.T) {
	checker := &mockInitChecker{needsInit: false, fullyInitialized: true}
	mw := InitMiddleware(checker)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// These paths should pass through when initialized
	passThroughPaths := []string{
		"/api/machines",
		"/api/certificates",
		"/api/users",
		"/api/dashboard",
		"/health",
	}

	for _, path := range passThroughPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("expected status 200 for path %s, got %d", path, w.Code)
			}
		})
	}

	// /init/* should return 403 when fully initialized
	blockedPaths := []string{
		"/init/status",
		"/init/admin",
		"/init/config",
	}

	for _, path := range blockedPaths {
		t.Run(path+"_blocked", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("expected status 403 for path %s when fully initialized, got %d", path, w.Code)
			}
		})
	}
}

func TestInitMiddleware_Returns500OnCheckerError(t *testing.T) {
	checker := &mockInitChecker{needsInit: false, fullyInitialized: false, err: context.DeadlineExceeded}
	mw := InitMiddleware(checker)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/machines", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 on checker error, got %d", w.Code)
	}
}
