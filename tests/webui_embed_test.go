package tests

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssl-manager/ssl-manager/webui"
)

func TestWebUIEmbedIncludesLeadingUnderscoreAssets(t *testing.T) {
	assetDir := filepath.Join("..", "webui", "dist", "assets")
	entries, err := os.ReadDir(assetDir)
	if err != nil {
		t.Fatalf("read built webui assets: %v", err)
	}

	var underscoreAssets []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "_") {
			underscoreAssets = append(underscoreAssets, filepath.ToSlash(filepath.Join("dist", "assets", entry.Name())))
		}
	}
	if len(underscoreAssets) == 0 {
		t.Skip("current webui build has no leading-underscore assets")
	}

	for _, asset := range underscoreAssets {
		if _, err := fs.Stat(webui.DistFS, asset); err != nil {
			t.Fatalf("built asset %q is missing from embedded webui FS: %v", asset, err)
		}
	}
}
