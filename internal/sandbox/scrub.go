// scrub.go removes secret values from text that is about to become an event,
// a log line, or a persisted field.
//
// The threat is not a malicious child: it is an ordinary one that echoes its own
// environment or an error message containing a token. Once that text is an
// event it is in the state file, in the SSE stream, and in the UI, so the value
// has to be removed before it becomes any of those.
package sandbox

import (
	"sort"
	"strings"
)

// Redacted replaces a secret value.
const Redacted = "[redacted]"

// minSecretLen is the shortest value worth redacting. Below this, a "secret" is
// more likely to be a common word and redacting it would mangle ordinary text
// without protecting anything.
const minSecretLen = 8

// Scrubber replaces known secret values in text. The zero value is a no-op, so a
// caller with no secrets pays nothing.
type Scrubber struct {
	// values are the secret strings, longest first so a token that contains a
	// shorter token is replaced whole rather than in pieces.
	values []string
}

// NewScrubber builds a Scrubber from secret values. Values shorter than
// minSecretLen are ignored, and duplicates are collapsed.
func NewScrubber(values ...string) *Scrubber {
	seen := make(map[string]bool)
	var kept []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if len(v) < minSecretLen || seen[v] {
			continue
		}
		seen[v] = true
		kept = append(kept, v)
	}
	sort.Slice(kept, func(i, j int) bool { return len(kept[i]) > len(kept[j]) })
	return &Scrubber{values: kept}
}

// Len is the number of values being scrubbed.
func (s *Scrubber) Len() int {
	if s == nil {
		return 0
	}
	return len(s.values)
}

// Scrub replaces every known secret value in text. It returns the text unchanged
// when there is nothing to do, so the common path allocates nothing.
func (s *Scrubber) Scrub(text string) string {
	if s == nil || len(s.values) == 0 || text == "" {
		return text
	}
	out := text
	for _, v := range s.values {
		if strings.Contains(out, v) {
			out = strings.ReplaceAll(out, v, Redacted)
		}
	}
	return out
}

// ScrubEnvValues builds a Scrubber from the values of environment entries whose
// name looks like a secret. This is how the executor protects itself without a
// list of every provider's variable name: the names it must never leak are the
// ones it already knows how to inject.
func ScrubEnvValues(env []string) *Scrubber {
	var values []string
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || value == "" {
			continue
		}
		if looksSecret(name) {
			values = append(values, value)
		}
	}
	return NewScrubber(values...)
}

// secretNameParts are the name fragments that mark a variable as carrying a
// credential. Matching is case-insensitive on the name only.
var secretNameParts = []string{
	"KEY", "TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL", "AUTH",
}

// looksSecret reports whether an environment name looks like it holds a
// credential.
func looksSecret(name string) bool {
	upper := strings.ToUpper(name)
	for _, part := range secretNameParts {
		if strings.Contains(upper, part) {
			return true
		}
	}
	return false
}
