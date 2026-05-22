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
	templates map[string]*template.Template
	staticFS  embed.FS
}

// NewWebUIHandler creates a new WebUIHandler using the provided embedded filesystems.
// templatesFS should contain files at "templates/*.html".
// staticFS should contain files at "static/css/..." and "static/js/...".
func NewWebUIHandler(templatesFS embed.FS, staticFS embed.FS) (*WebUIHandler, error) {
	// Parse layout template
	layoutContent, err := fs.ReadFile(templatesFS, "templates/layout.html")
	if err != nil {
		return nil, err
	}

	// Get all page template files
	entries, err := fs.ReadDir(templatesFS, "templates")
	if err != nil {
		return nil, err
	}

	templates := make(map[string]*template.Template)
	for _, entry := range entries {
		name := entry.Name()
		if name == "layout.html" || entry.IsDir() {
			continue
		}

		pageContent, err := fs.ReadFile(templatesFS, "templates/"+name)
		if err != nil {
			return nil, err
		}

		// Standalone pages (login, init) don't use the layout
		if name == "login.html" || name == "init.html" {
			tmpl, err := template.New(name).Parse(string(pageContent))
			if err != nil {
				return nil, err
			}
			templates[name] = tmpl
			continue
		}

		// Pages that use the layout: parse layout first, then the page
		tmpl, err := template.New("layout").Parse(string(layoutContent))
		if err != nil {
			return nil, err
		}

		_, err = tmpl.New(name).Parse(string(pageContent))
		if err != nil {
			return nil, err
		}

		templates[name] = tmpl
	}

	return &WebUIHandler{templates: templates, staticFS: staticFS}, nil
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
		tmpl, ok := h.templates[name]
		if !ok {
			http.Error(w, "Page not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		// Pages that use the layout template are executed via "layout" block.
		// Standalone pages (login, init) are executed directly by their filename.
		var err error
		if tmpl.Lookup("layout") != nil && name != "login.html" && name != "init.html" {
			err = tmpl.ExecuteTemplate(w, "layout", nil)
		} else {
			err = tmpl.Execute(w, nil)
		}

		if err != nil {
			if strings.Contains(err.Error(), "is undefined") {
				http.Error(w, "Page not found", http.StatusNotFound)
				return
			}
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}
}
