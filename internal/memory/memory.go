// Package memory is a bot's bounded memory: a small set of durable facts it
// carries into every run, and a dated log of what it actually did.
//
// The design rule is that memory never degrades silently. A fact that does not
// fit is refused with an error naming the usage, so the caller (a person in the
// UI, or the agent through its own tool) can remove something first. The old
// behaviour — clip the note and lose the tail — is exactly what makes a bot
// forget the thing you told it.
package memory

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// DefaultCap is the byte budget for one bot's facts. It is deliberately
	// small: facts ride in every prompt, so they compete with the task for
	// context. A bot that needs more should keep it in a skill.
	DefaultCap = 4 << 10

	// MaxLogEntries bounds the dated work log. The log answers "what did you do
	// last week"; runs keep their own events.
	MaxLogEntries = 200

	// maxFactLen bounds a single fact so one paste cannot consume the budget.
	maxFactLen = 512

	// maxSummaryLen bounds one log line.
	maxSummaryLen = 240

	// factPrefix marks a fact line in the rendered form.
	factPrefix = "- "
)

// ErrFull is returned (wrapped in *FullError) when a fact does not fit.
var ErrFull = errors.New("memory full")

// ErrNotFound is returned when a patch or remove matches no fact.
var ErrNotFound = errors.New("memory fact not found")

// FullError reports that a change would exceed the cap. It carries the usage so
// the caller can report "you are at 97% of 4096 bytes" instead of guessing.
type FullError struct {
	Used int
	Cap  int
}

func (e *FullError) Error() string {
	pct := 0
	if e.Cap > 0 {
		pct = e.Used * 100 / e.Cap
	}
	return fmt.Sprintf("memory full: %d of %d bytes used (%d%%); remove or patch a fact before adding another", e.Used, e.Cap, pct)
}

// Is lets callers test errors.Is(err, ErrFull).
func (e *FullError) Is(target error) bool { return target == ErrFull }

// leakTokens mirrors the store's persistence guard. The state file is refused
// if it contains any of these substrings anywhere, so accepting one into memory
// would break every later save — a failure far from its cause. Reject it here,
// where the message can name the token.
var leakTokens = []string{`"owner"`, "llm_", "api_key", "secret"}

// FindLeak returns the first persistence-guard token contained in s, or "".
func FindLeak(s string) string {
	low := strings.ToLower(s)
	for _, tok := range leakTokens {
		if strings.Contains(low, tok) {
			return tok
		}
	}
	return ""
}

// LogEntry is one dated line of what the bot did.
type LogEntry struct {
	At      string `json:"at"`
	RunID   string `json:"run_id,omitempty"`
	Summary string `json:"summary"`
}

// Facts is the bounded set of durable statements a bot carries into every run.
// The zero value is usable and means "no facts".
type Facts struct {
	lines []string
	cap   int
}

// ParseFacts reads the persisted form: one fact per line, each optionally
// prefixed with "- ". Blank lines are ignored and duplicates are dropped.
func ParseFacts(raw string, capacity int) Facts {
	if capacity <= 0 {
		capacity = DefaultCap
	}
	f := Facts{cap: capacity}
	seen := make(map[string]struct{})
	for _, line := range strings.Split(raw, "\n") {
		text := normalizeFact(line)
		if text == "" {
			continue
		}
		if _, dup := seen[text]; dup {
			continue
		}
		seen[text] = struct{}{}
		f.lines = append(f.lines, text)
	}
	return f
}

// normalizeFact trims the bullet prefix and surrounding space from one line.
func normalizeFact(line string) string {
	text := strings.TrimSpace(line)
	text = strings.TrimPrefix(text, "- ")
	return strings.TrimSpace(text)
}

// String renders the facts for persistence and for the prompt.
func (f Facts) String() string {
	if len(f.lines) == 0 {
		return ""
	}
	out := make([]string, len(f.lines))
	for i, line := range f.lines {
		out[i] = factPrefix + line
	}
	return strings.Join(out, "\n")
}

// Lines returns a copy of the facts.
func (f Facts) Lines() []string {
	return append([]string(nil), f.lines...)
}

// Len is the number of facts.
func (f Facts) Len() int { return len(f.lines) }

// Used is the byte budget currently consumed by the rendered form.
func (f Facts) Used() int { return len(f.String()) }

// Cap is the byte budget.
func (f Facts) Cap() int {
	if f.cap <= 0 {
		return DefaultCap
	}
	return f.cap
}

// Percent is Used as an integer percentage of Cap, for display.
func (f Facts) Percent() int {
	c := f.Cap()
	if c <= 0 {
		return 0
	}
	return f.Used() * 100 / c
}

// Add appends one fact and returns the new set. It refuses rather than clipping:
// a fact that would push the set past its cap yields *FullError carrying the
// usage, so the caller can act on it.
func (f Facts) Add(text string) (Facts, error) {
	text = normalizeFact(text)
	if text == "" {
		return f, fmt.Errorf("memory: fact required")
	}
	if tok := FindLeak(text); tok != "" {
		return f, fmt.Errorf("memory: fact contains %q, which the state file refuses to persist", tok)
	}
	if len(text) > maxFactLen {
		return f, fmt.Errorf("memory: fact is %d bytes, limit is %d", len(text), maxFactLen)
	}
	for _, existing := range f.lines {
		if existing == text {
			return f, nil
		}
	}
	next := Facts{lines: append(f.Lines(), text), cap: f.Cap()}
	if next.Used() > next.Cap() {
		return f, &FullError{Used: next.Used(), Cap: next.Cap()}
	}
	return next, nil
}

// Patch replaces the fact at index i. Out-of-range is ErrNotFound so a stale
// index from the agent cannot silently edit a different fact.
func (f Facts) Patch(i int, text string) (Facts, error) {
	if i < 0 || i >= len(f.lines) {
		return f, ErrNotFound
	}
	text = normalizeFact(text)
	if text == "" {
		return f, fmt.Errorf("memory: fact required")
	}
	if tok := FindLeak(text); tok != "" {
		return f, fmt.Errorf("memory: fact contains %q, which the state file refuses to persist", tok)
	}
	if len(text) > maxFactLen {
		return f, fmt.Errorf("memory: fact is %d bytes, limit is %d", len(text), maxFactLen)
	}
	lines := f.Lines()
	lines[i] = text
	next := Facts{lines: lines, cap: f.Cap()}
	if next.Used() > next.Cap() {
		return f, &FullError{Used: next.Used(), Cap: next.Cap()}
	}
	return next, nil
}

// Remove drops the fact at index i.
func (f Facts) Remove(i int) (Facts, error) {
	if i < 0 || i >= len(f.lines) {
		return f, ErrNotFound
	}
	lines := f.Lines()
	lines = append(lines[:i], lines[i+1:]...)
	return Facts{lines: lines, cap: f.Cap()}, nil
}

// WithCap returns the same facts under a different budget. It does not evict:
// shrinking below current usage leaves the set over cap so the next Add refuses
// and the caller sees the real number.
func (f Facts) WithCap(capacity int) Facts {
	if capacity <= 0 {
		capacity = DefaultCap
	}
	return Facts{lines: f.Lines(), cap: capacity}
}

// Log is a bot's dated work log, newest last.
type Log struct {
	entries []LogEntry
}

// NewLog builds a log from persisted entries, dropping entries with no summary
// and keeping only the most recent MaxLogEntries.
func NewLog(entries []LogEntry) Log {
	out := make([]LogEntry, 0, len(entries))
	for _, e := range entries {
		summary := strings.TrimSpace(e.Summary)
		if summary == "" {
			continue
		}
		out = append(out, LogEntry{At: e.At, RunID: e.RunID, Summary: clipText(summary, maxSummaryLen)})
	}
	if len(out) > MaxLogEntries {
		out = append(out[:0], out[len(out)-MaxLogEntries:]...)
	}
	return Log{entries: out}
}

// Entries returns a copy of the log.
func (l Log) Entries() []LogEntry {
	return append([]LogEntry(nil), l.entries...)
}

// Len is the number of entries.
func (l Log) Len() int { return len(l.entries) }

// Append records one entry, evicting the oldest past MaxLogEntries.
func (l Log) Append(at, runID, summary string) Log {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return l
	}
	// The marker must not itself contain the token, or the state file would
	// still refuse to save.
	if FindLeak(summary) != "" {
		summary = "[redacted]"
	}
	next := append(l.Entries(), LogEntry{
		At:      at,
		RunID:   runID,
		Summary: clipText(summary, maxSummaryLen),
	})
	if len(next) > MaxLogEntries {
		next = append(next[:0], next[len(next)-MaxLogEntries:]...)
	}
	return Log{entries: next}
}

// Recent returns up to n of the newest entries, oldest first.
func (l Log) Recent(n int) []LogEntry {
	if n <= 0 || n > len(l.entries) {
		n = len(l.entries)
	}
	return append([]LogEntry(nil), l.entries[len(l.entries)-n:]...)
}

// Render formats the newest n entries as prompt lines.
func (l Log) Render(n int) string {
	recent := l.Recent(n)
	if len(recent) == 0 {
		return ""
	}
	out := make([]string, 0, len(recent))
	for _, e := range recent {
		line := e.Summary
		if e.At != "" {
			line = e.At + " — " + line
		}
		out = append(out, "- "+line)
	}
	return strings.Join(out, "\n")
}

// clipText truncates to n bytes without splitting a rune.
func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
