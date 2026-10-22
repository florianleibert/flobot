// credentials.go loads the Muse API key from ~/.accounts/meta.ai and
// exposes it to child processes only as the MODEL_API_KEY environment
// variable. The value is never logged, printed, or persisted: error
// messages name the file but never its contents, and the key is only
// ever written into a child environment slice.
package executor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ModelAPIKeyEnv is the child-process environment variable that carries
// the Muse API key. It is the only place the key may appear.
const ModelAPIKeyEnv = "MODEL_API_KEY"

// MuseKeyRel is the key file location, relative to the user home dir.
const MuseKeyRel = ".accounts/meta.ai"

// ErrModelAPIKeyMissing is returned when the key file does not exist or
// is empty. Its message names the file but never any of its contents.
var ErrModelAPIKeyMissing = errors.New("model API key not found: create ~/.accounts/meta.ai (plain text, one line)")

// scrubKey removes the MODEL_API_KEY value from any text (child stderr can
// echo the environment on a crash). Returns the text unchanged when the key
// is empty or absent.
func scrubKey(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, "[redacted]")
}

// ReadModelAPIKey reads the plain-text key from <home>/.accounts/meta.ai.
// On a missing or empty file it returns ErrModelAPIKeyMissing (wrapped
// with the file path) without leaking any file contents.
func ReadModelAPIKey(home string) (string, error) {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	path := filepath.Join(home, MuseKeyRel)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s", ErrModelAPIKeyMissing, path)
		}
		// Do not surface the underlying OS error: it can embed file
		// details. The path is safe to name; the contents are not.
		return "", fmt.Errorf("%w: %s", ErrModelAPIKeyMissing, path)
	}
	key := strings.TrimSpace(string(b))
	if key == "" {
		return "", fmt.Errorf("%w: %s", ErrModelAPIKeyMissing, path)
	}
	return key, nil
}

// WithModelAPIKey returns a copy of env with MODEL_API_KEY set to key.
// The input slice is never mutated. The key appears only in the returned
// child environment; callers must not log, print, or persist it.
func WithModelAPIKey(env []string, key string) []string {
	out := append([]string(nil), env...)
	p := ModelAPIKeyEnv + "="
	for i, e := range out {
		if strings.HasPrefix(e, p) {
			out[i] = p + key
			return out
		}
	}
	return append(out, p+key)
}
