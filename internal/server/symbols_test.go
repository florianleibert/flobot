package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// symbolResponse mirrors the GET /api/v1/workspace/symbols payload.
type symbolResponse struct {
	Query   string `json:"query"`
	Symbols []struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"symbols"`
	Truncated bool `json:"truncated"`
}

func writeWorkspaceFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", abs, err)
	}
}

func TestWorkspaceSymbolsSearch(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	writeWorkspaceFile(t, work, "pkg/alpha.go", "package pkg\n\nfunc Widget() {}\n")
	writeWorkspaceFile(t, work, "pkg/beta.go", "package pkg\n\nfunc WidgetFactory() {}\n")
	writeWorkspaceFile(t, work, "pkg/gamma.go", "package pkg\n\nfunc Unrelated() {}\n")

	srv := New(nil, WithHome(home), WithWorkDir(work))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/api/v1/workspace/symbols?q=widget")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", res.StatusCode)
	}
	var out symbolResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Query != "widget" {
		t.Errorf("query = %q, want %q", out.Query, "widget")
	}
	if out.Truncated {
		t.Errorf("truncated = true, want false")
	}
	if len(out.Symbols) != 2 {
		t.Fatalf("got %d symbols, want 2: %+v", len(out.Symbols), out.Symbols)
	}
	// Exact match first, then the substring match.
	if out.Symbols[0].Name != "Widget" {
		t.Errorf("symbols[0].name = %q, want %q", out.Symbols[0].Name, "Widget")
	}
	if out.Symbols[0].Path != "pkg/alpha.go" {
		t.Errorf("symbols[0].path = %q, want %q", out.Symbols[0].Path, "pkg/alpha.go")
	}
	if out.Symbols[0].Kind != "func" {
		t.Errorf("symbols[0].kind = %q, want %q", out.Symbols[0].Kind, "func")
	}
	if out.Symbols[0].Line != 3 {
		t.Errorf("symbols[0].line = %d, want 3", out.Symbols[0].Line)
	}
	if out.Symbols[1].Name != "WidgetFactory" {
		t.Errorf("symbols[1].name = %q, want %q", out.Symbols[1].Name, "WidgetFactory")
	}
}

func TestWorkspaceSymbolsEmptyQueryReturnsEmptyList(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	writeWorkspaceFile(t, work, "a.go", "package a\n\nfunc Widget() {}\n")

	srv := New(nil, WithHome(home), WithWorkDir(work))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/api/v1/workspace/symbols?q=")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", res.StatusCode)
	}
	var out symbolResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Symbols) != 0 {
		t.Fatalf("got %d symbols, want 0 (empty query must not return the workspace)", len(out.Symbols))
	}
	if out.Truncated {
		t.Errorf("truncated = true, want false")
	}
}

func TestWorkspaceSymbolsNoMatchReturnsEmptyList(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	writeWorkspaceFile(t, work, "a.go", "package a\n\nfunc Widget() {}\n")

	srv := New(nil, WithHome(home), WithWorkDir(work))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/api/v1/workspace/symbols?q=zzzznotthere")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", res.StatusCode)
	}
	var out symbolResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Symbols) != 0 {
		t.Fatalf("got %d symbols, want 0", len(out.Symbols))
	}
}
