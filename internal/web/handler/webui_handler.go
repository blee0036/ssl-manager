package handler

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// WebUIHandler serves the web management frontend.
type WebUIHandler struct {
	templates *template.Template
	staticFS  embed.FS
}

// NewWebUIHandler creates a new WebUIHandler using the provided embedded filesystems.
// templatesFS should contain files at "templates/*.html".
// staticFS should contain files at "static/css/..." and "static/js/...".
func NewWebUIHandler(templatesFS embed.FS, staticFS embed.FS) (*WebUIHandler, error) {
	tmpl, err := template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &WebUIHandler{templates: tmpl, staticFS: staticFS}, nil
}

// RegisterRoutes registers the web UI routes on the given router.
func (h *WebUIHandler) RegisterRoutes(r chi.Router) {
	// Static files
	staticSub, _ := fs.Sub(h.staticFS, "static")
	fileServer := http.FileServer(http.FS(staticSub))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Page routes
	r.Get("/", h.handleIndex)
	r.Get("/init", h.renderPage("init.html"))
	r.Get("/login", h.renderPage("login.html"))
	r.Get("/dashboard", h.renderPage("dashboard.html"))
	r.Get("/certificates", h.renderPage("certificates.html"))
	r.Get("/machines", h.renderPage("machines.html"))
	r.Get("/domains", h.renderPage("domains.html"))
	r.Get("/thirdpart-dns", h.renderPage("thirdpart-dns.html"))
	r.Get("/alerts", h.renderPage("alerts.html"))
	r.Get("/audit-logs", h.renderPage("audit-logs.html"))
	r.Get("/system", h.renderPage("system.html"))
	r.Get("/users", h.renderPage("users.html"))
}

func (h *WebUIHandler) handleIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (h *WebUIHandler) renderPage(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := h.templates.ExecuteTemplate(w, name, nil); err != nil {
			if strings.Contains(err.Error(), "is undefined") {
				http.Error(w, "Page not found", http.StatusNotFound)
				return
			}
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}
}
