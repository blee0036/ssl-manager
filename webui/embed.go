// Package webui provides the embedded SPA frontend build artifacts.
package webui

import "embed"

// DistFS contains the built frontend assets from webui/dist/.
// This includes index.html, favicon.svg, and all files under assets/.
// Vite emits helper chunks with leading underscores, so use all:dist to keep
// Go embed from excluding them.
//
//go:embed all:dist
var DistFS embed.FS
