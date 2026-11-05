// Package sandbox keeps a run from reaching what it has no business reaching:
// it narrows the environment handed to the child, scrubs secret values out of
// anything that becomes an event, and generates a macOS Seatbelt profile for
// runs that should not touch the network or the home directory.
//
// The three concerns are separate on purpose. An allowlist is a policy about
// names; scrubbing is a policy about values that already leaked; Seatbelt is
// enforcement by the kernel. Only the last one is a boundary — the first two are
// what stop the boundary from being needed.
package sandbox

import (
	"sort"
	"strings"
)

// allowExact is the set of environment names a run inherits. Everything else is
// dropped, so a token that happens to be exported in the operator's shell does
// not silently become available to a scheduled routine.
//
// The list is deliberately about the process, not the person: PATH and locale so
// tools work, TMPDIR so temp files land somewhere writable, and the flocode
// variables the child is documented to read.
var allowExact = map[string]bool{
	"PATH":              true,
	"HOME":              true,
	"TMPDIR":            true,
	"LANG":              true,
	"LC_ALL":            true,
	"LC_CTYPE":          true,
	"TERM":              true,
	"TZ":                true,
	"SHELL":             true,
	"USER":              true,
	"LOGNAME":           true,
	"PWD":               true,
	"FLOCODE_LOG_LEVEL": true,
	"FLOCODE_CONFIG":    true,
	"FLOCODE_HOME":      true,
}

// allowPrefix lists name prefixes a run may inherit. These are the variables
// flobot itself sets for the child; a prefix keeps the list from growing every
// time one is added.
var allowPrefix = []string{
	"FLOCODE_",
}

// AllowEnv filters env down to the allowlist, then appends extra verbatim.
// Later entries win, so an explicit extra can override an inherited value.
//
// A name is kept when it is on allowExact or starts with an allowPrefix. Values
// are never inspected here: a name-based policy that also guessed at values
// would be a second, weaker secret detector.
func AllowEnv(env, extra []string) []string {
	out := make([]string, 0, len(env)+len(extra))
	for _, kv := range env {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || name == "" {
			continue
		}
		if allowExact[name] || hasAllowPrefix(name) {
			out = append(out, kv)
		}
	}
	out = append(out, extra...)
	return out
}

func hasAllowPrefix(name string) bool {
	for _, p := range allowPrefix {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// DroppedEnv returns the names AllowEnv would remove, sorted. It exists so a run
// can report "3 variables withheld" instead of leaving the operator to wonder
// why a tool cannot see its token.
func DroppedEnv(env []string) []string {
	var dropped []string
	for _, kv := range env {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || name == "" {
			continue
		}
		if !allowExact[name] && !hasAllowPrefix(name) {
			dropped = append(dropped, name)
		}
	}
	sort.Strings(dropped)
	return dropped
}
