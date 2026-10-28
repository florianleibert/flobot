package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Text search bounds. The search walks a real workspace, so it is bounded on
// every axis: how many files it opens, how much of each file it reads, and
// how many hits it returns. truncated reports that a bound cut the result
// short, so the UI can say "showing the first 200" instead of implying
// completeness.
const (
	maxSearchFiles   = 2000
	maxSearchBytes   = 512 << 10
	maxSearchResults = 200
	maxSearchLineLen = 240
)

// SearchMatch is one hit: the file, the line, the column and the text of the
// line (trimmed for display).
type SearchMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
	Text string `json:"text"`
}

// SearchText finds literal, case-insensitive matches of query in the
// workspace.
//
// scope, when not empty, limits the search to those workspace-relative files
// or folders — a project's scope, or the projects behind a tag. That is what
// makes "search inside #frontend" one click instead of a grep incantation.
//
// Symlinks are never followed: a symlink can point out of the workspace, and
// the search must never walk out of the tree.
func (w *Workspace) SearchText(query string, scope []string) (matches []SearchMatch, truncated bool, err error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []SearchMatch{}, false, nil
	}
	lower := strings.ToLower(query)

	root := w.root
	if r, e := filepath.EvalSymlinks(w.root); e == nil {
		root = r
	}
	scope = cleanScope(scope)

	files := 0
	out := []SearchMatch{}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory must not abort the whole search.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path == root {
				return nil
			}
			if strings.EqualFold(name, ".git") || ArtifactDirs[name] {
				return fs.SkipDir
			}
			if strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		relPath := filepath.ToSlash(rel)
		if len(scope) > 0 && !inScope(relPath, scope) {
			return nil
		}
		if files >= maxSearchFiles {
			truncated = true
			return fs.SkipAll
		}
		raw, rerr := readCapped(path, maxSearchBytes)
		if rerr != nil || len(raw) == 0 || isBinary(raw) {
			return nil
		}
		files++
		for i, line := range strings.Split(string(raw), "\n") {
			idx := strings.Index(strings.ToLower(line), lower)
			if idx < 0 {
				continue
			}
			out = append(out, SearchMatch{Path: relPath, Line: i + 1, Col: idx + 1, Text: clipSearchLine(line)})
			if len(out) >= maxSearchResults {
				truncated = true
				return fs.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil && walkErr != fs.SkipAll {
		return nil, truncated, fmt.Errorf("workspace: text search: %w", walkErr)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Line < out[j].Line
	})
	return out, truncated, nil
}

// cleanScope normalises scope paths: workspace-relative, no parent escapes.
// A path that tries to climb out is dropped, not clamped.
func cleanScope(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		p = strings.TrimPrefix(p, "./")
		p = strings.Trim(p, "/")
		if p == "" || p == "." {
			continue
		}
		parts := strings.Split(p, "/")
		clean := make([]string, 0, len(parts))
		for _, part := range parts {
			if part == "" || part == "." {
				continue
			}
			if part == ".." {
				clean = nil
				break
			}
			clean = append(clean, part)
		}
		if len(clean) == 0 {
			continue
		}
		out = append(out, strings.Join(clean, "/"))
	}
	return out
}

// inScope reports whether a workspace-relative path is inside the scope.
func inScope(rel string, scope []string) bool {
	for _, s := range scope {
		if rel == s || strings.HasPrefix(rel, s+"/") {
			return true
		}
	}
	return false
}

// clipSearchLine trims a matched line to a readable length.
func clipSearchLine(s string) string {
	s = strings.TrimRight(s, "\r")
	if len(s) > maxSearchLineLen {
		return s[:maxSearchLineLen] + "…"
	}
	return s
}
