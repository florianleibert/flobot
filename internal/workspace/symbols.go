package workspace

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	// maxSymbolFiles caps how many files a single workspace symbol search
	// may open. A search must stay interactive on a large tree, so the walk
	// is bounded rather than exhaustive.
	maxSymbolFiles = 2000
	// maxSymbolBytes caps the bytes read from one file during a search.
	// Symbols live in declarations near the top of a file; reading a whole
	// 2 MiB file to find one function name is wasted work.
	maxSymbolBytes = 512 << 10
	// maxSymbolResults caps the number of matches returned.
	maxSymbolResults = 200
)

// Symbol kinds. These are the values the UI renders as badges and the
// values the frontend outline rules already use.
const (
	SymbolFunc      = "func"
	SymbolType      = "type"
	SymbolConst     = "const"
	SymbolVar       = "var"
	SymbolClass     = "class"
	SymbolInterface = "interface"
	SymbolEnum      = "enum"
)

// Symbol is one declaration found in a workspace file.
type Symbol struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// symbolRule maps a declaration line to a symbol.
type symbolRule struct {
	re   *regexp.Regexp
	kind string
}

// symbolRules are per-language declaration patterns. The Go patterns
// mirror the frontend outline rules in web/app.js (OUTLINE_RULES) so the
// workspace-wide search and the open-file outline agree on what a symbol
// is and what it is called.
var symbolRules = map[string][]symbolRule{
	"go": {
		{regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)`), SymbolFunc},
		{regexp.MustCompile(`^\s*type\s+([A-Za-z_]\w*)`), SymbolType},
		{regexp.MustCompile(`^\s*const\s+([A-Za-z_]\w*)`), SymbolConst},
		{regexp.MustCompile(`^\s*var\s+([A-Za-z_]\w*)`), SymbolVar},
	},
	"js": {
		{regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][\w$]*)`), SymbolFunc},
		{regexp.MustCompile(`^\s*(?:export\s+)?(?:abstract\s+)?class\s+([A-Za-z_$][\w$]*)`), SymbolClass},
		{regexp.MustCompile(`^\s*(?:export\s+)?interface\s+([A-Za-z_$][\w$]*)`), SymbolInterface},
		{regexp.MustCompile(`^\s*(?:export\s+)?type\s+([A-Za-z_$][\w$]*)`), SymbolType},
		{regexp.MustCompile(`^\s*(?:export\s+)?(?:const\s+)?enum\s+([A-Za-z_$][\w$]*)`), SymbolEnum},
		{regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)`), SymbolConst},
	},
	"py": {
		{regexp.MustCompile(`^\s*def\s+([A-Za-z_]\w*)`), SymbolFunc},
		{regexp.MustCompile(`^\s*class\s+([A-Za-z_]\w*)`), SymbolClass},
	},
	"rs": {
		{regexp.MustCompile(`^\s*(?:pub\s+)?(?:async\s+)?fn\s+([A-Za-z_]\w*)`), SymbolFunc},
		{regexp.MustCompile(`^\s*(?:pub\s+)?struct\s+([A-Za-z_]\w*)`), SymbolType},
		{regexp.MustCompile(`^\s*(?:pub\s+)?enum\s+([A-Za-z_]\w*)`), SymbolEnum},
		{regexp.MustCompile(`^\s*(?:pub\s+)?trait\s+([A-Za-z_]\w*)`), SymbolInterface},
		{regexp.MustCompile(`^\s*(?:pub\s+)?type\s+([A-Za-z_]\w*)`), SymbolType},
	},
	"sh": {
		{regexp.MustCompile(`^\s*(?:function\s+)?([A-Za-z_]\w*)\s*\(\s*\)\s*\{`), SymbolFunc},
	},
}

// symbolLang maps a file extension to a symbolRules key.
var symbolLang = map[string]string{
	".go": "go",
	".js": "js", ".jsx": "js", ".mjs": "js", ".cjs": "js",
	".ts": "js", ".tsx": "js",
	".py": "py",
	".rs": "rs",
	".sh": "sh", ".bash": "sh",
}

// ExtractSymbols returns the declarations in one file's content, in file
// order. The path is used only to pick the language; an unknown extension
// yields no symbols rather than a guess.
func ExtractSymbols(path, content string) []Symbol {
	lang, ok := symbolLang[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return nil
	}
	rules := symbolRules[lang]
	var out []Symbol
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		for _, rule := range rules {
			m := rule.re.FindStringSubmatch(text)
			if m == nil {
				continue
			}
			out = append(out, Symbol{Line: line, Kind: rule.kind, Name: m[1]})
			break
		}
	}
	return out
}

// symbolMatch is a symbol plus its relevance score. Lower is better.
type symbolMatch struct {
	sym   Symbol
	score int
}

// scoreSymbol ranks a name against a lowercase query:
// 0 exact, 1 case-insensitive exact, 2 prefix, 3 substring.
// It returns -1 when the name does not match at all.
func scoreSymbol(name, query, lowerQuery string) int {
	switch {
	case name == query:
		return 0
	case strings.ToLower(name) == lowerQuery:
		return 1
	case strings.HasPrefix(strings.ToLower(name), lowerQuery):
		return 2
	case strings.Contains(strings.ToLower(name), lowerQuery):
		return 3
	}
	return -1
}

// SearchSymbols finds declarations whose name matches query across the
// workspace. The match is a case-insensitive substring; an empty query
// returns no results rather than the whole workspace.
//
// The search is bounded: at most maxSymbolFiles files are opened, at most
// maxSymbolBytes are read from each, and at most maxSymbolResults matches
// are returned. truncated reports that a bound cut the result short, so
// the UI can say "showing the first 200" instead of implying completeness.
//
// Symlinks are skipped entirely: a symlink can point outside the
// workspace root, and the search must never follow one out of the tree.
func (w *Workspace) SearchSymbols(query string) (syms []Symbol, truncated bool, err error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []Symbol{}, false, nil
	}
	lowerQuery := strings.ToLower(query)

	root := w.root
	if r, e := filepath.EvalSymlinks(w.root); e == nil {
		root = r
	}

	files := 0
	matches := []symbolMatch{}
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
		// Symlinks are never followed out of the workspace.
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if _, ok := symbolLang[strings.ToLower(filepath.Ext(name))]; !ok {
			return nil
		}
		if files >= maxSymbolFiles {
			truncated = true
			return fs.SkipAll
		}
		files++

		raw, rerr := readCapped(path, maxSymbolBytes)
		if rerr != nil || len(raw) == 0 {
			return nil
		}
		if isBinary(raw) {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		relPath := filepath.ToSlash(rel)
		for _, s := range ExtractSymbols(relPath, string(raw)) {
			score := scoreSymbol(s.Name, query, lowerQuery)
			if score < 0 {
				continue
			}
			s.Path = relPath
			matches = append(matches, symbolMatch{sym: s, score: score})
		}
		return nil
	})
	if walkErr != nil && walkErr != fs.SkipAll {
		return nil, truncated, fmt.Errorf("workspace: symbol search: %w", walkErr)
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score < matches[j].score
		}
		if matches[i].sym.Path != matches[j].sym.Path {
			return matches[i].sym.Path < matches[j].sym.Path
		}
		return matches[i].sym.Line < matches[j].sym.Line
	})

	if len(matches) > maxSymbolResults {
		matches = matches[:maxSymbolResults]
		truncated = true
	}
	syms = make([]Symbol, 0, len(matches))
	for _, m := range matches {
		syms = append(syms, m.sym)
	}
	return syms, truncated, nil
}

// readCapped reads at most limit bytes from path. A short read is not an
// error; a file larger than the cap yields its first limit bytes, which is
// enough to find declarations and keeps the walk cheap.
func readCapped(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit))
}
