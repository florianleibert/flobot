package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearchTextFindsMatches(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/alpha.go", "package pkg\n\nfunc Widget() {}\n")
	writeFile(t, root, "pkg/notes.md", "the widget is here\nand the widget again\n")
	writeFile(t, root, "other/beta.go", "package other\n")

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	matches, truncated, err := ws.SearchText("widget", nil)
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	if truncated {
		t.Error("truncated = true, want false")
	}
	// One hit per matching line: alpha.go once, notes.md twice.
	if len(matches) != 3 {
		t.Fatalf("matches = %+v, want 3", matches)
	}
	// Hits are ordered by path, then line.
	if matches[0].Path != "pkg/alpha.go" || matches[0].Line != 3 {
		t.Errorf("matches[0] = %+v, want pkg/alpha.go:3", matches[0])
	}
	if matches[1].Path != "pkg/notes.md" || matches[1].Line != 1 {
		t.Errorf("matches[1] = %+v, want pkg/notes.md:1", matches[1])
	}
	if matches[2].Line != 2 {
		t.Errorf("matches[2].Line = %d, want 2", matches[2].Line)
	}
}

func TestSearchTextIsCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package a\n\nfunc Widget() {}\n")
	ws, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	matches, _, err := ws.SearchText("WIDGET", nil)
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %+v, want 1", matches)
	}
	if matches[0].Col != 6 {
		t.Errorf("matches[0].Col = %d, want 6", matches[0].Col)
	}
}

func TestSearchTextScopeLimitsTheSearch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/a.go", "package pkg\n\nfunc Widget() {}\n")
	writeFile(t, root, "docs/b.md", "widget\n")

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	scoped, _, err := ws.SearchText("widget", []string{"pkg"})
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	if len(scoped) != 1 || scoped[0].Path != "pkg/a.go" {
		t.Fatalf("scoped = %+v, want only pkg/a.go", scoped)
	}
	// A scope that is a file, not a folder, works too.
	file, _, err := ws.SearchText("widget", []string{"docs/b.md"})
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	if len(file) != 1 || file[0].Path != "docs/b.md" {
		t.Fatalf("file scope = %+v, want only docs/b.md", file)
	}
}

func TestSearchTextDropsEscapingScope(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package a\n\nfunc Widget() {}\n")
	ws, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// A scope that climbs out of the workspace is dropped, not clamped: the
	// search then covers the workspace instead of escaping it.
	matches, _, err := ws.SearchText("widget", []string{"../.."})
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %+v, want 1 (the scope is dropped, not honoured)", matches)
	}
}

func TestSearchTextEmptyQueryReturnsNothing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package a\n\nfunc Widget() {}\n")
	ws, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, q := range []string{"", "   ", "\t"} {
		matches, truncated, err := ws.SearchText(q, nil)
		if err != nil {
			t.Fatalf("SearchText(%q): %v", q, err)
		}
		if len(matches) != 0 {
			t.Errorf("SearchText(%q) = %+v, want no matches", q, matches)
		}
		if truncated {
			t.Errorf("SearchText(%q) truncated = true, want false", q)
		}
	}
}

func TestSearchTextSkipsArtifactDirsAndSymlinks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "keep.go", "package a\n\nfunc Kept() {}\n")
	writeFile(t, root, "node_modules/dep.go", "package dep\n\nfunc Skipped() {}\n")
	writeFile(t, root, ".hidden/secret.go", "package hidden\n\nfunc Skipped() {}\n")

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "leak.go"), []byte("package leak\n\nfunc Leaked() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	matches, _, err := ws.SearchText("skipped", nil)
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("matches = %+v, want none from artifact or dot dirs", matches)
	}
	leak, _, err := ws.SearchText("leaked", nil)
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	if len(leak) != 0 {
		t.Fatalf("matches = %+v, want none through a symlink out of the workspace", leak)
	}
}
