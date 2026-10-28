package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(abs), err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", abs, err)
	}
}

func TestExtractSymbolsGo(t *testing.T) {
	content := `package main

import "fmt"

// Helper does a thing.
func Helper() string { return "x" }

func (l *Loop) Method(a int) int { return a }

type Widget struct{ Name string }

const MaxWidgets = 10

var defaultName = "widget"
`
	got := ExtractSymbols("main.go", content)
	want := []struct {
		line int
		kind string
		name string
	}{
		{6, SymbolFunc, "Helper"},
		{8, SymbolFunc, "Method"},
		{10, SymbolType, "Widget"},
		{12, SymbolConst, "MaxWidgets"},
		{14, SymbolVar, "defaultName"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d symbols, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Line != w.line || got[i].Kind != w.kind || got[i].Name != w.name {
			t.Errorf("symbol %d = {line:%d kind:%q name:%q}, want {line:%d kind:%q name:%q}",
				i, got[i].Line, got[i].Kind, got[i].Name, w.line, w.kind, w.name)
		}
	}
}

func TestExtractSymbolsLanguages(t *testing.T) {
	cases := []struct {
		path    string
		content string
		kind    string
		name    string
	}{
		{"a.ts", "export class Widget {}", SymbolClass, "Widget"},
		{"a.ts", "export interface Shape { r: number }", SymbolInterface, "Shape"},
		{"a.ts", "export type Alias = string", SymbolType, "Alias"},
		{"a.ts", "export enum Color { Red }", SymbolEnum, "Color"},
		{"a.ts", "export const value = 1", SymbolConst, "value"},
		{"a.js", "export async function go() {}", SymbolFunc, "go"},
		{"a.py", "def greet():\n    pass", SymbolFunc, "greet"},
		{"a.py", "class Robot:\n    pass", SymbolClass, "Robot"},
		{"a.rs", "pub fn run() {}", SymbolFunc, "run"},
		{"a.rs", "pub struct Engine {}", SymbolType, "Engine"},
		{"a.rs", "pub trait Runner {}", SymbolInterface, "Runner"},
		{"a.sh", "build() {\n  echo hi\n}", SymbolFunc, "build"},
	}
	for _, tc := range cases {
		got := ExtractSymbols(tc.path, tc.content)
		if len(got) == 0 {
			t.Errorf("%s: no symbols extracted from %q", tc.path, tc.content)
			continue
		}
		found := false
		for _, s := range got {
			if s.Kind == tc.kind && s.Name == tc.name {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: want symbol %s/%s, got %+v", tc.path, tc.kind, tc.name, got)
		}
	}
}

func TestExtractSymbolsUnknownExtension(t *testing.T) {
	if got := ExtractSymbols("notes.txt", "func Nope() {}"); got != nil {
		t.Fatalf("unknown extension should yield no symbols, got %+v", got)
	}
}

func TestSearchSymbolsAcrossFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/alpha.go", "package pkg\n\nfunc Widget() {}\n")
	writeFile(t, root, "pkg/beta.go", "package pkg\n\nfunc WidgetHelper() {}\n")
	writeFile(t, root, "pkg/gamma.go", "package pkg\n\nfunc Other() {}\n")

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	syms, truncated, err := ws.SearchSymbols("widget")
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	if truncated {
		t.Errorf("truncated = true, want false")
	}
	if len(syms) != 2 {
		t.Fatalf("got %d symbols, want 2: %+v", len(syms), syms)
	}
	// Exact match ranks before the substring match.
	if syms[0].Name != "Widget" {
		t.Errorf("syms[0].Name = %q, want %q (exact match first)", syms[0].Name, "Widget")
	}
	if syms[0].Path != "pkg/alpha.go" {
		t.Errorf("syms[0].Path = %q, want %q", syms[0].Path, "pkg/alpha.go")
	}
	if syms[1].Name != "WidgetHelper" {
		t.Errorf("syms[1].Name = %q, want %q", syms[1].Name, "WidgetHelper")
	}
	if syms[0].Kind != SymbolFunc {
		t.Errorf("syms[0].Kind = %q, want %q", syms[0].Kind, SymbolFunc)
	}
	if syms[0].Line != 3 {
		t.Errorf("syms[0].Line = %d, want 3", syms[0].Line)
	}
}

func TestSearchSymbolsEmptyQueryReturnsNothing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package a\n\nfunc Widget() {}\n")
	ws, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, q := range []string{"", "   ", "\t"} {
		syms, truncated, err := ws.SearchSymbols(q)
		if err != nil {
			t.Fatalf("SearchSymbols(%q): %v", q, err)
		}
		if len(syms) != 0 {
			t.Errorf("SearchSymbols(%q) = %+v, want empty", q, syms)
		}
		if truncated {
			t.Errorf("SearchSymbols(%q) truncated = true, want false", q)
		}
	}
}

func TestSearchSymbolsSkipsArtifactDirsAndDotDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "keep.go", "package a\n\nfunc Kept() {}\n")
	writeFile(t, root, "node_modules/dep.go", "package dep\n\nfunc Skipped() {}\n")
	writeFile(t, root, ".hidden/secret.go", "package hidden\n\nfunc Skipped() {}\n")

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	syms, _, err := ws.SearchSymbols("skipped")
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	if len(syms) != 0 {
		t.Fatalf("got %+v, want no symbols from artifact or dot directories", syms)
	}
	syms, _, err = ws.SearchSymbols("kept")
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	if len(syms) != 1 || syms[0].Name != "Kept" {
		t.Fatalf("got %+v, want exactly Kept", syms)
	}
}

func TestSearchSymbolsDoesNotFollowSymlinkOutOfRoot(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "leak.go"), []byte("package leak\n\nfunc Leaked() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	root := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	ws, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	syms, _, err := ws.SearchSymbols("leaked")
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	if len(syms) != 0 {
		t.Fatalf("got %+v, want no symbols through a symlink out of the workspace", syms)
	}
}

func TestSearchSymbolsNoMatchReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package a\n\nfunc Widget() {}\n")
	ws, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	syms, _, err := ws.SearchSymbols("nonexistent")
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	if len(syms) != 0 {
		t.Fatalf("got %+v, want empty", syms)
	}
}
