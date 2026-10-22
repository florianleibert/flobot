// Package diff parses unified diffs into per-file hunks and applies
// selected hunks to file content with exact-then-fuzzy matching
// (Aider-informed: flexible application beats strict formats).
package diff

import (
	"fmt"
	"strings"
)

// Line is one diff line.
type Line struct {
	Type string `json:"type"` // ctx | del | add
	Text string `json:"text"`
}

// Hunk is one @@ section of a file's diff.
type Hunk struct {
	Header string `json:"header"`
	Lines  []Line `json:"lines"`
	// OldStart is the 1-based line number in the pre-image parsed
	// from the @@ header; 0 when absent. It biases content matching
	// so a hunk whose pattern occurs twice lands at the right one.
	OldStart int `json:"-"`
	// NoNewlineOld/New are set when a "\ No newline at end of file"
	// marker follows a del/ctx (old) or add/ctx (new) line of this
	// hunk: the pre-/post-image ends without a trailing newline.
	NoNewlineOld bool `json:"-"`
	NoNewlineNew bool `json:"-"`
}

// FileDiff is one file's diff.
type FileDiff struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath,omitempty"`
	OldMode   string `json:"oldMode,omitempty"`
	NewMode   string `json:"newMode,omitempty"`
	Hunks     []Hunk `json:"hunks"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	IsNew     bool   `json:"isNew,omitempty"`
	IsDelete  bool   `json:"isDelete,omitempty"`
	IsRename  bool   `json:"isRename,omitempty"`
	IsBinary  bool   `json:"isBinary,omitempty"`
	// State is the review state (pending|accepted|rejected), set by the
	// review API, not by the parser.
	State string `json:"state,omitempty"`
}

// Totals aggregates a diff.
type Totals struct {
	Files     int `json:"files"`
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

// TotalsOf sums a file diff list.
func TotalsOf(fds []FileDiff) Totals {
	var t Totals
	for _, f := range fds {
		t.Files++
		t.Additions += f.Additions
		t.Deletions += f.Deletions
	}
	return t
}

// Parse parses a unified diff (git diff-tree -r output) into file diffs.
func Parse(unified string) []FileDiff {
	var out []FileDiff
	var cur *FileDiff
	var hunk *Hunk
	// git output ends with a newline; a trailing "" from Split would
	// otherwise append a phantom empty context line to the last hunk.
	unified = strings.TrimSuffix(unified, "\n")
	for _, line := range strings.Split(unified, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			if cur != nil {
				if hunk != nil {
					cur.Hunks = append(cur.Hunks, *hunk)
				}
				out = append(out, finalize(*cur))
			}
			cur = &FileDiff{}
			hunk = nil
			// A mode-only change has no ---/+++ headers; its paths come
			// only from the "diff --git a/x b/x" line.
			if a, b, ok := parseDiffGitPaths(line); ok {
				cur.OldPath = a
				cur.Path = b
			}
		case cur == nil:
			continue
		case hunk != nil:
			// Inside a hunk, content lines win: a deleted line whose
			// text starts with "-- " renders as "--- x" and must not
			// be mistaken for a file header (same for "++ " adds).
			switch {
			case strings.HasPrefix(line, "+"):
				hunk.Lines = append(hunk.Lines, Line{Type: "add", Text: line[1:]})
				cur.Additions++
			case strings.HasPrefix(line, "-"):
				hunk.Lines = append(hunk.Lines, Line{Type: "del", Text: line[1:]})
				cur.Deletions++
			case strings.HasPrefix(line, " "):
				hunk.Lines = append(hunk.Lines, Line{Type: "ctx", Text: line[1:]})
			case line == "":
				// git omits the leading space on empty context lines
				hunk.Lines = append(hunk.Lines, Line{Type: "ctx", Text: ""})
			case strings.HasPrefix(line, "\\"):
				// "\ No newline at end of file" marker: not content. It
				// applies to the hunk's last line so far: a del/ctx line
				// means the pre-image lacks the newline, an add/ctx line
				// means the post-image does.
				if n := len(hunk.Lines); n > 0 {
					switch hunk.Lines[n-1].Type {
					case "add":
						hunk.NoNewlineNew = true
					case "del":
						hunk.NoNewlineOld = true
					case "ctx":
						hunk.NoNewlineOld = true
						hunk.NoNewlineNew = true
					}
				}
				// "\ No newline at end of file" — marker, not content
			case strings.HasPrefix(line, "@@"):
				cur.Hunks = append(cur.Hunks, *hunk)
				hunk = &Hunk{Header: line, OldStart: parseOldStart(line)}
			default:
				// Malformed line: stop accumulating this hunk.
				cur.Hunks = append(cur.Hunks, *hunk)
				hunk = nil
			}
		case strings.HasPrefix(line, "@@"):
			hunk = &Hunk{Header: line, OldStart: parseOldStart(line)}
		case strings.HasPrefix(line, "--- "):
			cur.OldPath = cleanPath(line[4:])
		case strings.HasPrefix(line, "+++ "):
			p := cleanPath(line[4:])
			if p != "" && p != "/dev/null" {
				cur.Path = p
			}
		case strings.HasPrefix(line, "old mode "):
			cur.OldMode = strings.TrimSpace(line[9:])
		case strings.HasPrefix(line, "new mode "):
			cur.NewMode = strings.TrimSpace(line[9:])
		case strings.HasPrefix(line, "rename from "):
			cur.OldPath = strings.TrimSpace(line[12:])
			cur.IsRename = true
		case strings.HasPrefix(line, "rename to "):
			cur.Path = strings.TrimSpace(line[10:])
			cur.IsRename = true
		case strings.HasPrefix(line, "Binary files"):
			cur.IsBinary = true
		case strings.HasPrefix(line, "new file mode"):
			cur.IsNew = true
		case strings.HasPrefix(line, "deleted file mode"):
			cur.IsDelete = true
		}
	}
	if cur != nil {
		if hunk != nil {
			cur.Hunks = append(cur.Hunks, *hunk)
		}
		out = append(out, finalize(*cur))
	}
	return out
}

// finalize fills in fallbacks: a deleted file has no +++ b/ line, so its
// Path comes from the --- a/ side.
func finalize(fd FileDiff) FileDiff {
	if fd.Path == "" && fd.OldPath != "" && fd.OldPath != "/dev/null" {
		fd.Path = fd.OldPath
	}
	return fd
}

// parseDiffGitPaths extracts the a/ and b/ paths from a
// "diff --git a/x b/x" line. Paths containing spaces are quoted
// ("a/some file"), so both forms are handled.
func parseDiffGitPaths(line string) (a, b string, ok bool) {
	rest := strings.TrimPrefix(line, "diff --git ")
	var parts []string
	if strings.HasPrefix(rest, "\"") {
		parts = splitQuoted(rest)
	} else {
		parts = strings.Fields(rest)
	}
	if len(parts) < 2 {
		return "", "", false
	}
	return cleanPath(parts[0]), cleanPath(parts[1]), true
}

func splitQuoted(s string) []string {
	var out []string
	for len(s) > 0 {
		if s[0] != '"' {
			s = s[1:]
			continue
		}
		end := strings.IndexByte(s[1:], '"')
		if end < 0 {
			break
		}
		out = append(out, s[1:1+end])
		s = s[2+end:]
	}
	return out
}

func cleanPath(p string) string {
	p = strings.TrimSpace(p)
	if i := strings.Index(p, "\t"); i >= 0 {
		p = p[:i]
	}
	if p == "/dev/null" {
		return p
	}
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		return p[2:]
	}
	return p
}

// ApplyResult reports what happened for one file.
type ApplyResult struct {
	Path    string `json:"path"`
	Applied []int  `json:"applied"`
	Skipped []int  `json:"skipped"`
	Content string `json:"-"`
	Exists  bool   `json:"-"`
}

// Apply applies the selected hunks (by index) of one file to content.
// Matching is exact first; on failure, fuzzy strategies in order:
// trim leading/trailing context, whitespace-normalized match, sub-hunk
// split. A hunk that cannot apply is reported in Skipped, never silently
// dropped.
func Apply(path, content string, exists bool, fd FileDiff, indices []int) ApplyResult {
	res := ApplyResult{Path: path, Content: content, Exists: exists}
	if fd.IsDelete {
		// Deleting the file is the whole-file operation.
		if len(indices) > 0 {
			res.Applied = indices
			res.Content = ""
			res.Exists = false
		}
		return res
	}
	if fd.IsNew {
		// New file: content is the added lines of selected hunks.
		var b strings.Builder
		lastIdx := -1
		for _, i := range indices {
			if i < 0 || i >= len(fd.Hunks) {
				res.Skipped = append(res.Skipped, i)
				continue
			}
			for _, ln := range fd.Hunks[i].Lines {
				if ln.Type == "add" {
					b.WriteString(ln.Text)
					b.WriteString("\n")
				}
			}
			res.Applied = append(res.Applied, i)
			lastIdx = i
		}
		res.Content = b.String()
		if lastIdx >= 0 && fd.Hunks[lastIdx].NoNewlineNew {
			res.Content = strings.TrimSuffix(res.Content, "\n")
		}
		res.Exists = true
		return res
	}
	lines := strings.Split(content, "\n")
	// Apply hunks in ascending order; each is re-located by content
	// search in the mutated slice, so earlier hunks cannot invalidate
	// later offsets.
	sel := make([]int, 0, len(indices))
	seen := make(map[int]bool, len(indices))
	for _, i := range indices {
		if i < 0 || i >= len(fd.Hunks) {
			res.Skipped = append(res.Skipped, i)
			continue
		}
		if seen[i] {
			continue // duplicate index: apply once, not twice
		}
		seen[i] = true
		sel = append(sel, i)
	}
	for a := 0; a < len(sel); a++ {
		for b := a + 1; b < len(sel); b++ {
			if sel[b] < sel[a] {
				sel[a], sel[b] = sel[b], sel[a]
			}
		}
	}
	// delta accumulates the line-count change of previously applied
	// hunks so each hunk's @@ pre-image hint stays meaningful in the
	// mutated slice (an earlier hunk that adds k lines shifts every
	// later position by k).
	delta := 0
	for _, i := range sel {
		h := fd.Hunks[i]
		out, ok := applyHunk(lines, h, delta)
		if !ok {
			res.Skipped = append(res.Skipped, i)
			continue
		}
		delta += len(out) - len(lines)
		lines = out
		res.Applied = append(res.Applied, i)
	}
	res.Content = strings.Join(lines, "\n")
	// Trailing-newline semantics: Split/Join preserves the pre-image's
	// newline state by construction; the diff's "\ No newline" markers
	// override it when the run changed the last line's newline.
	oldNoNL, newNoNL := false, false
	for _, i := range sel {
		if fd.Hunks[i].NoNewlineNew {
			newNoNL = true
		}
		if fd.Hunks[i].NoNewlineOld {
			oldNoNL = true
		}
	}
	if newNoNL {
		res.Content = strings.TrimSuffix(res.Content, "\n")
	} else if oldNoNL && !strings.HasSuffix(content, "\n") {
		// Pre-image ended without a newline, post-image ends with one.
		res.Content += "\n"
	}
	res.Exists = true
	return res
}

// applyHunk applies one hunk to lines, exact then fuzzy. hint is the
// hunk's pre-image position adjusted by delta (the cumulative line
// shift of hunks already applied).
func applyHunk(lines []string, h Hunk, delta int) ([]string, bool) {
	if out, ok := applyExact(lines, h, delta); ok {
		return out, true
	}
	if out, ok := applyFuzzy(lines, h, delta); ok {
		return out, true
	}
	return nil, false
}

// applyExact finds the del+ctx sequence verbatim in lines. When the
// hunk header carries a pre-image line number, the occurrence nearest
// that position wins, so a pattern that occurs twice (duplicate
// blocks) lands at the right one.
func applyExact(lines []string, h Hunk, delta int) ([]string, bool) {
	pat := make([]string, 0, len(h.Lines))
	for _, ln := range h.Lines {
		if ln.Type == "add" {
			continue
		}
		pat = append(pat, ln.Text)
	}
	if len(pat) == 0 {
		// Pure addition with no anchor. On an empty (or absent) file
		// the adds are the whole content; otherwise we cannot place it.
		if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") ||
			(len(lines) == 2 && lines[0] == "" && lines[1] == "") {
			// Empty or absent file (a lone "\n" splits to ["",""]).
			// Preserve the pre-image's trailing newline: Join of the
			// adds alone would drop it.
			out := addsOnly(h)
			if n := len(lines); n > 0 && lines[n-1] == "" {
				out = append(out, "")
			}
			return out, true
		}
		return nil, false
	}
	idx := findSubNear(lines, pat, h.OldStart-1+delta)
	if idx < 0 {
		return nil, false
	}
	return splice(lines, idx, len(pat), h)
}

// addsOnly returns just the hunk's add lines.
func addsOnly(h Hunk) []string {
	out := make([]string, 0, len(h.Lines))
	for _, ln := range h.Lines {
		if ln.Type == "add" {
			out = append(out, ln.Text)
		}
	}
	return out
}

// applyFuzzy trims leading/trailing context lines and retries, then
// falls back to whitespace-normalized matching.
func applyFuzzy(lines []string, h Hunk, delta int) ([]string, bool) {
	// Strategy 1: trim context.
	for trim := 1; trim <= len(h.Lines); trim++ {
		hh := trimHunk(h, trim)
		if len(hh.Lines) == 0 {
			break
		}
		if out, ok := applyExact(lines, hh, delta); ok {
			return out, true
		}
	}
	// Strategy 2: whitespace-normalized match.
	pat := make([]string, 0, len(h.Lines))
	for _, ln := range h.Lines {
		if ln.Type == "add" {
			continue
		}
		pat = append(pat, strings.TrimSpace(ln.Text))
	}
	if len(pat) == 0 {
		return nil, false
	}
	norm := make([]string, len(lines))
	for i, l := range lines {
		norm[i] = strings.TrimSpace(l)
	}
	idx := findSubNear(norm, pat, h.OldStart-1+delta)
	if idx < 0 {
		return nil, false
	}
	return splice(lines, idx, len(pat), h)
}

// trimHunk drops up to trim leading and trailing ctx lines.
func trimHunk(h Hunk, trim int) Hunk {
	lines := h.Lines
	lead := 0
	for lead < trim && lead < len(lines) && lines[lead].Type == "ctx" {
		lead++
	}
	tail := 0
	for tail < trim && lead+tail < len(lines) && lines[len(lines)-1-tail].Type == "ctx" {
		tail++
	}
	h.Lines = lines[lead : len(lines)-tail]
	return h
}

func findSub(hay, pat []string) int {
	if len(pat) > len(hay) {
		return -1
	}
	for i := 0; i+len(pat) <= len(hay); i++ {
		match := true
		for j := range pat {
			if hay[i+j] != pat[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// findSubNear returns the occurrence of pat in hay closest to hint
// (0-based). hint < 0 means first occurrence. When the pattern occurs
// more than once, the @@ header's pre-image line number disambiguates.
func findSubNear(hay, pat []string, hint int) int {
	if hint < 0 {
		return findSub(hay, pat)
	}
	best := -1
	bestDist := 0
	for i := 0; i+len(pat) <= len(hay); i++ {
		match := true
		for j := range pat {
			if hay[i+j] != pat[j] {
				match = false
				break
			}
		}
		if match {
			d := i - hint
			if d < 0 {
				d = -d
			}
			if best < 0 || d < bestDist {
				best, bestDist = i, d
			}
		}
	}
	return best
}

// parseOldStart extracts the pre-image start line from an @@ header
// ("@@ -5,3 +1,3 @@" → 5). 0 when absent or malformed.
func parseOldStart(header string) int {
	h := strings.TrimSpace(strings.TrimPrefix(header, "@@"))
	if !strings.HasPrefix(h, "-") {
		return 0
	}
	h = h[1:]
	n, i := 0, 0
	for ; i < len(h) && h[i] >= '0' && h[i] <= '9'; i++ {
		n = n*10 + int(h[i]-'0')
	}
	if i == 0 {
		return 0
	}
	return n
}

// splice replaces lines[idx:idx+n] with the hunk's output — context lines
// are preserved, del lines consumed, add lines emitted.
func splice(lines []string, idx, n int, h Hunk) ([]string, bool) {
	out := make([]string, 0, len(lines))
	out = append(out, lines[:idx]...)
	consumed := 0
	for _, ln := range h.Lines {
		switch ln.Type {
		case "ctx":
			// Emit the file's own context line, not the diff's text:
			// under whitespace-normalized (fuzzy) matching the file's
			// indentation may differ, and splicing the diff's text
			// would silently de-indent the file. Under exact matching
			// the two are identical.
			out = append(out, lines[idx+consumed])
			consumed++
		case "del":
			consumed++
		case "add":
			out = append(out, ln.Text)
		}
	}
	if consumed != n {
		return nil, false
	}
	out = append(out, lines[idx+n:]...)
	return out, true
}

// ValidateSelection checks hunk indices against a file diff.
func ValidateSelection(fd FileDiff, sel []int) error {
	for _, i := range sel {
		if i < 0 || i >= len(fd.Hunks) {
			return fmt.Errorf("hunk index %d out of range for %s", i, fd.Path)
		}
	}
	return nil
}
