package middleware

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadBodyFragmentVerifiesBuiltSpa ensures a PJAX request to a freshly built
// SPA ships the <body> fragment (root mount point) from index.html, not the whole
// document. It skips when web/dist is absent (e.g. headless CI that never built).
func TestReadBodyFragmentVerifiesBuiltSpa(t *testing.T) {
	p := filepath.Join("..", "..", "web", "dist", "index.html")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("web/dist/index.html not built, skipping: %v", err)
	}
	html := string(raw)

	fragment, err := readBodyFragment(p)
	if err != nil {
		t.Fatalf("readBodyFragment: %v", err)
	}

	if !strings.Contains(fragment, `<div id="root">`) {
		t.Errorf("fragment is missing the #root mount point; got: %q", fragment)
	}
	// The fragment is a subset of the document: it must be meaningful but not
	// carry the document <html> wrapper (SPA client navigation handles routes).
	if strings.Contains(fragment, "<html") {
		t.Errorf("fragment unexpectedly contains the full document wrapper")
	}
	if fragment == "" {
		t.Fatalf("fragment is empty")
	}
	// Sanity: the fragment should be strictly smaller than the full html slice.
	if len(fragment) >= len(html) {
		t.Errorf("fragment is not smaller than the document (%d >= %d)", len(fragment), len(html))
	}
}
