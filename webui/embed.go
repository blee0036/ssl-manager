// Package webui provides the embedded SPA frontend build artifacts.
package webui

import "embed"

// DistFS contains the built frontend assets from webui/dist/.
// This includes index.html, favicon.svg, and all files under assets/.
//
//go:embed dist/*
var DistFS embed.FS
