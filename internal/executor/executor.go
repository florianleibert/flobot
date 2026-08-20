// Package executor spawns the flocode2 binary and parses streaming-json NDJSON.
package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	DefaultTimeout = 10 * time.Minute
	AskTimeout     = 60 * time.Second
	stderrCap      = 32 << 10
	textCap        = 8 << 10
	fallbackBin    = "/Users/floleibert/dev/flocode2/flocode"
	museKeyRel     = ".accounts/meta.ai"
)

// Event is one parsed NDJSON object. Only kind, turn_id, text, tool, is_error.
type Event struct {
	Kind    string `json:"kind"`
	TurnID  uint64 `json:"turn_id,omitempty"`
	Text    string `json:"text,omitempty"`
	Tool    string `json:"tool,omitempty"`
	IsError *bool  `json:"is_error,omitempty"`
}

// Request is one flocode child.
type Request struct {
	Prompt        string
	Pin           string
	Intent        string
	Yolo          bool
	Verify        bool
	Worktree      string
	ContinueUntil bool
	Route         string
	Timeout       time.Duration
	Dir           string
}

// Result is the captured stream. Stderr is a diagnostic ring and is never
// a secret-bearing API field.
type Result struct {
	Events []Event
	Stderr string
	Err    error
}

// Exec runs flocode. Bin, Environ, and Home are overridable in tests.
type Exec struct {
	Bin     string
	Environ func() []string
	Home    string
}

// FindBinary returns FLOCODE_BIN, then PATH flocode, then the flocode2 tree.
func FindBinary() (string, error) {
	if p := strings.TrimSpace(os.Getenv("FLOCODE_BIN")); p != "" {
		return p, nil
	}
	if p, err := exec.LookPath("flocode"); err == nil {
		return p, nil
	}
	if st, err := os.Stat(fallbackBin); err == nil && !st.IsDir() {
		return fallbackBin, nil
	}
	return "", fmt.Errorf("flocode binary not found (set FLOCODE_BIN)")
}

func (e *Exec) binary() (string, error) {
	if e != nil && e.Bin != "" {
		return e.Bin, nil
	}
	return FindBinary()
}

// Argv is the canonical flocode argv (no secrets).
func Argv(req Request) []string {
	intent := strings.TrimSpace(req.Intent)
	if intent == "" {
		intent = "code"
	}
	args := []string{
		"-p", req.Prompt,
		"--model", req.Pin,
		"--intent", intent,
		"--output", "streaming-json",
	}
	if req.Yolo {
		args = append(args, "--yolo")
	}
	if req.Verify {
		args = append(args, "--verify")
	}
	if wt := strings.TrimSpace(req.Worktree); wt != "" {
		args = append(args, "--worktree", wt)
	}
	if req.ContinueUntil {
		args = append(args, "--continue-until")
	}
	return args
}

func (e *Exec) timeout(req Request) time.Duration {
	if req.Timeout > 0 {
		return req.Timeout
	}
	return DefaultTimeout
}

// Run starts flocode, streams parsed events, and kills the process group on cancel.
func (e *Exec) Run(ctx context.Context, req Request, emit func(Event)) Result {
	if emit == nil {
		emit = func(Event) {}
	}
	bin, err := e.binary()
	if err != nil {
		ev := Event{Kind: "err", Text: err.Error()}
		emit(ev)
		return Result{Events: []Event{ev}, Err: err}
	}
	if strings.TrimSpace(req.Pin) == "" {
		err := fmt.Errorf("model pin required")
		ev := Event{Kind: "err", Text: err.Error()}
		emit(ev)
		return Result{Events: []Event{ev}, Err: err}
	}
	to := e.timeout(req)
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	args := Argv(req)
	cmd := exec.Command(bin, args...)
	if req.Dir != "" {
		cmd.Dir = req.Dir
	}
	cmd.Env = e.childEnv(req.Route)
	setPgid(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		ev := Event{Kind: "err", Text: err.Error()}
		emit(ev)
		return Result{Events: []Event{ev}, Err: err}
	}
	ring := &ringBuf{max: stderrCap}
	cmd.Stderr = ring
	if err := cmd.Start(); err != nil {
		ev := Event{Kind: "err", Text: err.Error()}
		emit(ev)
		return Result{Events: []Event{ev}, Err: err}
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			killGroup(cmd.Process)
		case <-done:
		}
	}()

	var events []Event
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), 256<<10)
	for sc.Scan() {
		ev, ok := parseLine(sc.Bytes())
		if !ok {
			continue
		}
		events = append(events, ev)
		emit(ev)
	}
	scanErr := sc.Err()
	waitErr := cmd.Wait()
	close(done)
	stderr := ring.String()

	if ctx.Err() != nil {
		err := ctx.Err()
		if len(events) == 0 || events[len(events)-1].Kind != "err" {
			ev := Event{Kind: "err", Text: err.Error()}
			events = append(events, ev)
			emit(ev)
		}
		return Result{Events: events, Stderr: stderr, Err: err}
	}
	if waitErr != nil {
		if len(events) == 0 || events[len(events)-1].Kind != "err" {
			ev := Event{Kind: "err", Text: "flocode exited: " + waitErr.Error()}
			events = append(events, ev)
			emit(ev)
		}
		return Result{Events: events, Stderr: stderr, Err: waitErr}
	}
	if scanErr != nil && scanErr != io.EOF {
		ev := Event{Kind: "err", Text: scanErr.Error()}
		events = append(events, ev)
		emit(ev)
		return Result{Events: events, Stderr: stderr, Err: scanErr}
	}
	return Result{Events: events, Stderr: stderr}
}

func parseLine(line []byte) (Event, bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return Event{}, false
	}
	var ev Event
	if err := json.Unmarshal(line, &ev); err != nil {
		return Event{}, false
	}
	if ev.Kind == "" {
		return Event{}, false
	}
	ev.Text = clip(ev.Text)
	ev.Tool = strings.TrimSpace(ev.Tool)
	return ev, true
}

func clip(s string) string {
	if len(s) <= textCap {
		return s
	}
	s = s[:textCap]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func (e *Exec) childEnv(route string) []string {
	var base []string
	if e != nil && e.Environ != nil {
		base = e.Environ()
	} else {
		base = os.Environ()
	}
	out := append([]string(nil), base...)
	if !envHas(out, "FLOCODE_LOG_LEVEL") {
		out = append(out, "FLOCODE_LOG_LEVEL=info")
	}
	if strings.TrimSpace(route) == "muse" && strings.TrimSpace(envVal(out, "MODEL_API_KEY")) == "" {
		if k := strings.TrimSpace(readMuseKey(e.home())); k != "" {
			out = envSet(out, "MODEL_API_KEY", k)
		}
	}
	return out
}

func (e *Exec) home() string {
	if e != nil && e.Home != "" {
		return e.Home
	}
	h, _ := os.UserHomeDir()
	return h
}

// LookupModelAPIKey returns MODEL_API_KEY from the environment, else the
// trimmed ~/.accounts/meta.ai file. Callers must not log the value.
func LookupModelAPIKey(home string) string {
	if v := strings.TrimSpace(os.Getenv("MODEL_API_KEY")); v != "" {
		return v
	}
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return readMuseKey(home)
}

func readMuseKey(home string) string {
	if home == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, museKeyRel))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func envHas(env []string, key string) bool {
	p := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, p) {
			return true
		}
	}
	return false
}

func envVal(env []string, key string) string {
	p := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, p) {
			return e[len(p):]
		}
	}
	return ""
}

func envSet(env []string, key, val string) []string {
	p := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, p) {
			env[i] = p + val
			return env
		}
	}
	return append(env, p+val)
}

type ringBuf struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (r *ringBuf) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.max <= 0 {
		return len(p), nil
	}
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.max {
		r.buf = append([]byte(nil), r.buf[len(r.buf)-r.max:]...)
	}
	return len(p), nil
}

func (r *ringBuf) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}
