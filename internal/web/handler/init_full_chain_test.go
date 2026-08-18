package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/database"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// initCheckerAdapter adapts *service.InitService to middleware.InitChecker,
// mirroring how cmd/web/main.go wires the real InitMiddleware.
type initCheckerAdapter struct {
	svc *service.InitService
}

func (a *initCheckerAdapter) NeedsInit(ctx context.Context) (bool, error) {
	phase, err := a.svc.GetPhase(ctx)
	if err != nil {
		return false, err
	}
	return phase != "completed", nil
}

func (a *initCheckerAdapter) IsFullyInitialized(ctx context.Context) (bool, error) {
	return a.svc.IsFullyInitialized(ctx)
}

// TestFullChain_UninitializedSystem_RedirectsAndServesInitPage reproduces the
// exact request chain a browser goes through on a fresh, uninitialized system:
//
//  1. GET / with Accept: text/html  -> InitMiddleware sees needsInit=true and
//     issues a 307 redirect to /init (this is intentional, documented behavior).
//  2. The browser follows the redirect: GET /init with Accept: text/html.
//
// This wires together InitMiddleware + InitHandler + WebUIHandler in the same
// registration order as cmd/web/main.go (global InitMiddleware first, then
// initHandler.RegisterRoutes, then the SPA catch-all last), so it validates the
// full chain end-to-end rather than each piece in isolation.
func TestFullChain_UninitializedSystem_RedirectsAndServesInitPage(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := database.NewDB(tmpDir)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(tmpDir, "config.json")
	initSvc := service.NewInitService(db, userRepo, configPath, nil)
	initHandler := NewInitHandler(initSvc)

	const indexContent = "<html><body>init-wizard-page</body></html>"
	fakeDist := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(indexContent)},
	}
	spaHandler := NewWebUIHandler(fakeDist)

	r := chi.NewRouter()
	// Same order as cmd/web/main.go: InitMiddleware is global, applied before
	// any route is registered, so it wraps init routes AND the SPA fallback.
	r.Use(middleware.InitMiddleware(&initCheckerAdapter{svc: initSvc}))
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	initHandler.RegisterRoutes(r)
	spaHandler.RegisterRoutes(r)

	// Step 1: browser hits the root page on an uninitialized system.
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.Header.Set("Accept", "text/html,application/xhtml+xml")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusTemporaryRedirect {
		t.Fatalf("step1: expected 307 redirect, got %d; body: %s", w1.Code, w1.Body.String())
	}
	location := w1.Header().Get("Location")
	if location != "/init" {
		t.Fatalf("step1: expected redirect Location=/init, got %q", location)
	}

	// Step 2: browser follows the redirect (a fresh request, as a browser would issue).
	req2 := httptest.NewRequest(http.MethodGet, location, nil)
	req2.Header.Set("Accept", "text/html,application/xhtml+xml")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("step2: expected 200 (init wizard page), got %d; body: %s", w2.Code, w2.Body.String())
	}
	if w2.Body.String() != indexContent {
		t.Errorf("step2: expected index.html content, got: %s", w2.Body.String())
	}

	// Sanity: the real API endpoint under /init/ must still work in this same setup.
	req3 := httptest.NewRequest(http.MethodGet, "/init/status", nil)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Errorf("expected /init/status 200, got %d; body: %s", w3.Code, w3.Body.String())
	}
}

// TestFullChain_FullyInitializedSystem_InitRoutesForbidden verifies that once
// the system is fully initialized, /init and /init/* are blocked with 403 by
// InitMiddleware (not silently served as the SPA index page), matching the
// documented "once initialized, /init/* returns 403" contract.
func TestFullChain_FullyInitializedSystem_InitRoutesForbidden(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := database.NewDB(tmpDir)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(tmpDir, "config.json")
	initSvc := service.NewInitService(db, userRepo, configPath, nil)
	initHandler := NewInitHandler(initSvc)

	_, initToken, err := initSvc.CreateAdmin(context.Background(), service.CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}
	if _, err := initSvc.SaveConfig(context.Background(), initToken, service.SaveConfigInput{}); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	fakeDist := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}
	spaHandler := NewWebUIHandler(fakeDist)

	r := chi.NewRouter()
	r.Use(middleware.InitMiddleware(&initCheckerAdapter{svc: initSvc}))
	initHandler.RegisterRoutes(r)
	spaHandler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/init", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for /init on fully initialized system, got %d; body: %s", w.Code, w.Body.String())
	}
}
