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
	// museKeyRel is the key file location, relative to the user home dir.
	museKeyRel = MuseKeyRel
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
	// SkillBody, when set, is prepended to Prompt as a how-to the child
	// must follow. It is prompt text, never a secret.
	SkillBody     string
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
	prompt := applySkillBody(req)
	intent := strings.TrimSpace(req.Intent)
	if intent == "" {
		intent = "code"
	}
	args := []string{
		"-p", prompt,
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

// applySkillBody returns the prompt with the skill how-to prepended when
// SkillBody is set. Argv and Run share this so the child always sees the
// same prompt.
func applySkillBody(req Request) string {
	if sb := strings.TrimSpace(req.SkillBody); sb != "" {
		return "Skill (follow this how-to):\n" + sb + "\n\n" + req.Prompt
	}
	return req.Prompt
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
	cmd.Env = e.childEnv(req.Route, req.Verify)
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

	// ccpm run log: one line per finished child. Never includes secrets.
	logCCPMRun(req.Dir, req.Pin, req.Intent, waitErr)

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

func (e *Exec) childEnv(route string, verify bool) []string {
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
	needKey := strings.TrimSpace(route) == "muse" || verify
	if needKey && strings.TrimSpace(envVal(out, ModelAPIKeyEnv)) == "" {
		if k, err := ReadModelAPIKey(e.home()); err == nil {
			out = WithModelAPIKey(out, k)
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

// ccpmRunsRel is the run log inside the workspace.
const ccpmRunsRel = "docs/ccpm-runs.md"

const ccpmRunsHeader = `# ccpm runs

One line per flocode child. Never contains API keys or tokens.

| time | pin | intent | result |
| --- | --- | --- | --- |
`

// logCCPMRun appends one markdown table row to docs/ccpm-runs.md in dir
// (or the current directory). It never writes API keys, MODEL_API_KEY or
// LLM_ tokens: only the pin, intent and a sanitized result are recorded.
func logCCPMRun(dir, pin, intent string, err error) {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	result := "ok"
	if err != nil {
		result = "error"
	}
	line := fmt.Sprintf("| %s | %s | %s | %s |\n",
		time.Now().UTC().Format(time.RFC3339),
		sanitizeCell(pin),
		sanitizeCell(intent),
		result,
	)
	path := filepath.Join(dir, ccpmRunsRel)
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			return
		}
		if wErr := os.WriteFile(path, []byte(ccpmRunsHeader), 0o644); wErr != nil {
			return
		}
	}
	f, openErr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if openErr != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// sanitizeCell keeps a value safe for a markdown table cell: no pipes,
// newlines, or anything that looks like a secret.
func sanitizeCell(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if s == "" {
		return "-"
	}
	return s
}
