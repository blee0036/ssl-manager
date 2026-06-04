package handler

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
)

// WebUIHandler serves the SPA frontend from an embedded or OS filesystem.
// It implements SPA fallback: if the requested file does not exist, it serves index.html
// so that client-side routing (Vue Router) can handle the path.
type WebUIHandler struct {
	distFS     fs.FS
	fileServer http.Handler
}

// NewWebUIHandler creates a new SPA handler using the provided filesystem.
// distFS should be rooted at the dist/ directory (i.e., index.html is at the root of distFS).
func NewWebUIHandler(distFS fs.FS) *WebUIHandler {
	return &WebUIHandler{
		distFS:     distFS,
		fileServer: http.FileServer(http.FS(distFS)),
	}
}

// RegisterRoutes registers the SPA catch-all route.
// This MUST be called AFTER all /api/*, /health, and /init/* routes are registered.
func (h *WebUIHandler) RegisterRoutes(r chi.Router) {
	r.Get("/*", h.serveSPA)
}

// serveSPA serves static files from the embedded filesystem.
// If the requested file exists, it is served directly with proper content type.
// If the file does not exist (SPA client route like /dashboard, /certificates),
// index.html is served so Vue Router can handle the route.
func (h *WebUIHandler) serveSPA(w http.ResponseWriter, r *http.Request) {
	// Clean the URL path
	urlPath := path.Clean(r.URL.Path)
	if urlPath == "/" {
		urlPath = "/index.html"
	}

	// Strip leading slash for fs operations
	filePath := strings.TrimPrefix(urlPath, "/")

	// Try to open the file from the embedded filesystem
	f, err := h.distFS.Open(filePath)
	if err != nil {
		// File not found — serve index.html for SPA fallback
		h.serveIndex(w, r)
		return
	}
	defer f.Close()

	// Check if it's a directory (e.g., /assets/) — serve index.html
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		h.serveIndex(w, r)
		return
	}

	// File exists — serve it via the file server
	h.fileServer.ServeHTTP(w, r)
}

// serveIndex serves the index.html file for SPA fallback.
func (h *WebUIHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	indexFile, err := fs.ReadFile(h.distFS, "index.html")
	if err != nil {
		// If index.html doesn't exist, something is very wrong
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Prevent caching of index.html so that new deployments are picked up
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)
	w.Write(indexFile)
}

// NewSPAHandler creates an http.Handler that serves SPA static files with fallback.
// This is a convenience function for use outside of chi router context.
func NewSPAHandler(distFS fs.FS) http.Handler {
	h := &WebUIHandler{
		distFS:     distFS,
		fileServer: http.FileServer(http.FS(distFS)),
	}
	return http.HandlerFunc(h.serveSPA)
}

// NewSPAHandlerFromPath creates an SPA handler from an OS directory path.
// Useful for development mode where files are served from disk instead of embed.
func NewSPAHandlerFromPath(dirPath string) http.Handler {
	return NewSPAHandler(os.DirFS(dirPath))
}
