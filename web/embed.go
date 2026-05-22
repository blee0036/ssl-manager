// Package web provides embedded static assets and templates for the web UI.
package web

import "embed"

// TemplatesFS contains the HTML templates.
//
//go:embed templates/*.html
var TemplatesFS embed.FS

// StaticFS contains the static assets (CSS, JS).
//
//go:embed static/css/*.css static/js/*.js
var StaticFS embed.FS
