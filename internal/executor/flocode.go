// flocode.go implements a minimal, self-contained flocode child runner.
// It resolves the flocode binary via PATH, spawns it with
// `-p <prompt> --output streaming-json`, and forwards each parsed NDJSON
// object on stdout as a map to a channel.
//
// This file intentionally does not import github.com/florianleibert/flocode:
// flocode is an external binary, not a Go dependency.
package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// Flocode runs the flocode binary. Bin overrides PATH resolution in tests.
type Flocode struct {
	// Bin, when non-empty, is used directly instead of PATH lookup.
	Bin string
	// Dir, when non-empty, is the child working directory.
	Dir string
	// Environ, when non-nil, supplies the child environment.
	Environ func() []string
}

// ResolveFlocode returns the flocode binary path from PATH.
func ResolveFlocode() (string, error) {
	p, err := exec.LookPath("flocode")
	if err != nil {
		return "", fmt.Errorf("flocode not found in PATH: %w", err)
	}
	return p, nil
}

// binary returns the configured binary or resolves flocode via PATH.
func (f *Flocode) binary() (string, error) {
	if f != nil && f.Bin != "" {
		return f.Bin, nil
	}
	return ResolveFlocode()
}

// Run spawns flocode with `-p <prompt> --output streaming-json` and forwards
// each parsed NDJSON object from stdout to out as a map. The channel is
// closed when the process exits or ctx is canceled. The returned error is
// the process exit error, if any.
func (f *Flocode) Run(ctx context.Context, prompt string, out chan<- map[string]any) error {
	bin, err := f.binary()
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, bin, "-p", prompt, "--output", "streaming-json")
	if f != nil && f.Dir != "" {
		cmd.Dir = f.Dir
	}
	if f != nil && f.Environ != nil {
		cmd.Env = f.Environ()
	} else {
		cmd.Env = os.Environ()
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	go func() {
		defer close(out)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64<<10), 256<<10)
		for sc.Scan() {
			m, ok := parseNDJSONLine(sc.Bytes())
			if !ok {
				continue
			}
			select {
			case out <- m:
			case <-ctx.Done():
				return
			}
		}
	}()

	return cmd.Wait()
}

// parseNDJSONLine parses one NDJSON line into a map. Blank lines and lines
// that are not JSON objects are skipped.
func parseNDJSONLine(line []byte) (map[string]any, bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, false
	}
	if m == nil {
		return nil, false
	}
	return m, true
}
