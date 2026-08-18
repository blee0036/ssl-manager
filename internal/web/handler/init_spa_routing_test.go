package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/database"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// TestInitHandler_BareInitPath_FallsThroughToSPA is a regression test for a bug
// where GET /init (no sub-path) returned a raw "404 page not found" from chi
// instead of being served index.html by the SPA fallback.
//
// Root cause: InitHandler used to register its routes via r.Route("/init", ...),
// which chi implements as Mount(). Mount() installs a real stub handler on the
// bare "/init" path (in addition to "/init/*") so it can dispatch into the
// sub-router. Since the sub-router only has handlers for /status, /admin, and
// /config, a request for the bare "/init" path matched the stub, found no
// matching sub-route, and chi's default NotFoundHandler answered directly —
// never reaching the WebUIHandler's catch-all "/*" route that would otherwise
// serve index.html so the Vue Router "/init" client-side page could render.
func TestInitHandler_BareInitPath_FallsThroughToSPA(t *testing.T) {
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

	// Minimal fake SPA dist filesystem containing only index.html.
	const indexContent = "<html><body>init-page</body></html>"
	fakeDist := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(indexContent)},
	}
	spaHandler := NewWebUIHandler(fakeDist)

	r := chi.NewRouter()
	// Registration order matters: init routes first, SPA catch-all last —
	// mirrors cmd/web/main.go's real registration order.
	initHandler.RegisterRoutes(r)
	spaHandler.RegisterRoutes(r)

	t.Run("bare /init serves index.html, not a raw 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/init", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200 (index.html via SPA fallback), got %d; body: %s", w.Code, w.Body.String())
		}
		if w.Body.String() != indexContent {
			t.Errorf("expected index.html content, got: %s", w.Body.String())
		}
	})

	t.Run("/init/status still resolves to the real API handler", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/init/status", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200 from InitHandler.GetStatus, got %d; body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("unknown /init/whatever does not leak the SPA index either", func(t *testing.T) {
		// Any unmatched sub-path under /init/ should 404, not silently serve
		// index.html (webui_handler.go's shouldReturnNotFound covers this).
		req := httptest.NewRequest(http.MethodGet, "/init/whatever", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected status 404 for unknown /init/* sub-path, got %d", w.Code)
		}
	})
}
