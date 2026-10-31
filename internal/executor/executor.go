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
	// fallbackBin is resolved at startup: $FLOCODE_BIN wins, else
	// $HOME/dev/flocode2/flocode if it exists. Never a hardcoded user path.
	fallbackBin = "dev/flocode2/flocode"
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
	Prompt string
	// SkillBody, when set, is prepended to Prompt as a how-to the child
	// must follow. It is prompt text, never a secret.
	SkillBody string
	Pin       string
	Intent    string
	// Tools is the tool policy for this run: "" (the intent's default),
	// "none" (no tools at all — the run can only answer in text) or a
	// comma-separated allow-list such as "read,glob,grep".
	Tools    string
	Yolo     bool
	Verify   bool
	Worktree string
	// ConfigOverride, when set, is written to a temp file and passed to the
	// child as FLOCODE_CONFIG. Used for mesh routes: a generated config that
	// pins the mesh model. It must never contain secrets.
	ConfigOverride string
	ContinueUntil  bool
	Route          string
	Timeout        time.Duration
	Dir            string
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
	home, _ := os.UserHomeDir()
	if home != "" {
		p := filepath.Join(home, fallbackBin)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
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
	// A local model on this Mac loads cold and streams slowly. flocode's
	// first-token and stall watchdogs (one minute by default) kill it
	// mid-answer, and the run dies with "provider timeout" while the model
	// was still thinking. A local route gets a wider window; a mesh route
	// keeps the defaults.
	if strings.TrimSpace(req.Route) == "local" {
		args = append(args, "--first-token-timeout", "6m", "--stall-timeout", "6m")
	}
	// The tool policy belongs to the run, not to the playbook's wording. A
	// local model is tool-happy: asked to "answer in plain text" it
	// sometimes returns a tool call instead of the answer, and the
	// automation looks broken. "none" starts the child with no tools at
	// all; a list narrows it to exactly those tools.
	switch tools := strings.ToLower(strings.TrimSpace(req.Tools)); {
	case tools == "none":
		args = append(args, "--no-tools")
	case tools != "":
		args = append(args, "--tool-allow", tools)
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
// A non-nil reqEnv overrides the computed child environment (used by Verify
// to inject MODEL_API_KEY exactly once).
func (e *Exec) Run(ctx context.Context, req Request, emit func(Event)) Result {
	return e.run(ctx, req, nil, emit)
}

func (e *Exec) run(ctx context.Context, req Request, reqEnv []string, emit func(Event)) Result {
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
	if reqEnv != nil {
		cmd.Env = reqEnv
	}
	if cfg := strings.TrimSpace(req.ConfigOverride); cfg != "" {
		path, err := writeConfigOverride(cfg)
		if err != nil {
			ev := Event{Kind: "err", Text: err.Error()}
			emit(ev)
			return Result{Events: []Event{ev}, Err: err}
		}
		defer os.Remove(path)
		cmd.Env = envSet(cmd.Env, "FLOCODE_CONFIG", path)
	}
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
			// killGroup SIGTERMs the whole process group (flocode spawns
			// tool children); the child exits within ms and cmd.Wait()
			// in the reader loop returns, so no goroutine leaks.
			killGroup(cmd.Process)
		case <-done:
		}
	}()

	var events []Event
	rd := bufio.NewReaderSize(stdout, 64<<10)
	for {
		line, err := readLine(rd, 256<<10)
		if len(line) > 0 {
			if ev, ok := parseLine(line); ok {
				events = append(events, ev)
				emit(ev)
			}
		}
		if err != nil {
			if err != io.EOF {
				scanErr := err
				_ = cmd.Wait()
				close(done)
				stderr := scrubKey(ring.String(), envVal(cmd.Env, ModelAPIKeyEnv))
				ev := Event{Kind: "err", Text: scanErr.Error()}
				events = append(events, ev)
				emit(ev)
				return Result{Events: events, Stderr: stderr, Err: scanErr}
			}
			break
		}
	}
	scanErr := error(nil)
	waitErr := cmd.Wait()
	close(done)
	stderr := scrubKey(ring.String(), envVal(cmd.Env, ModelAPIKeyEnv))

	// ccpm run log: one line per finished child. Never includes secrets.
	// The log lives in ~/.flobot, never in the user's workspace.
	e.logCCPMRun(req.Pin, req.Intent, waitErr)

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

// readLine reads one newline-terminated line, truncating any line longer
// than max bytes instead of failing (a single oversized NDJSON event must
// not kill the whole run). Returns io.EOF at end of input.
func readLine(rd *bufio.Reader, max int) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := rd.ReadSlice('\n')
		buf = append(buf, chunk...)
		if err == bufio.ErrBufferFull {
			if len(buf) > max {
				buf = buf[:max]
				// Skip the rest of the oversized line, up to the newline.
				for {
					chunk, err := rd.ReadSlice('\n')
					if err != bufio.ErrBufferFull {
						break
					}
					_ = chunk
				}
				return buf, nil
			}
			continue
		}
		if err != nil {
			if len(buf) > 0 && err == io.EOF {
				return buf, io.EOF
			}
			return buf, err
		}
		return buf, nil
	}
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

// writeConfigOverride writes a generated flocode config to a private temp
// file and returns its path. The file is removed when the child exits.
// The config must never contain secrets; mesh endpoints are private-network
// URLs that need no key.
func writeConfigOverride(cfg string) (string, error) {
	f, err := os.CreateTemp("", "flobot-mesh-*.toml")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(cfg); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	if err := os.Chmod(f.Name(), 0o600); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
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

// ccpmRunsFile is the run log inside ~/.flobot.
//
// It used to live in the workspace (docs/ccpm-runs.md). That put flobot's own
// bookkeeping into the user's repository: it dirtied the tree on every run,
// and — because the post-run snapshot is taken after the log is written — it
// showed up in the agent's diff as a change the user never made. The log
// belongs with flobot's other state, not in the user's project.
const ccpmRunsFile = "ccpm-runs.md"

const (
	// ccpmMaxBytes and ccpmMaxLines bound the log: an audit trail is useful
	// for the recent past, not forever.
	ccpmMaxBytes = 256 << 10
	ccpmMaxLines = 500
	// ccpmHeaderLines is the length of the markdown header (title, blank,
	// note, blank, table head).
	ccpmHeaderLines = 5
)

const ccpmRunsHeader = `# ccpm runs

One line per flocode child. Never contains API keys or tokens.

| time | pin | intent | result |
| --- | --- | --- | --- |
`

// logCCPMRun appends one markdown table row to ~/.flobot/ccpm-runs.md.
//
// It never writes API keys, MODEL_API_KEY or LLM_ tokens: only the pin,
// intent and a sanitized result are recorded. If the home directory cannot
// be resolved the line is dropped: the log must never fall back to the
// user's workspace.
func (e *Exec) logCCPMRun(pin, intent string, runErr error) {
	home := e.home()
	if home == "" {
		return
	}
	result := "ok"
	if runErr != nil {
		result = "error"
	}
	line := fmt.Sprintf("| %s | %s | %s | %s |\n",
		time.Now().UTC().Format(time.RFC3339),
		sanitizeCell(pin),
		sanitizeCell(intent),
		result,
	)
	dir := filepath.Join(home, ".flobot")
	path := filepath.Join(dir, ccpmRunsFile)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		if wErr := os.WriteFile(path, []byte(ccpmRunsHeader), 0o600); wErr != nil {
			return
		}
	}
	f, openErr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if openErr != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
	trimCCPMRuns(path)
}

// trimCCPMRuns keeps the log bounded: the header plus the newest lines.
func trimCCPMRuns(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= ccpmMaxBytes {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) <= ccpmHeaderLines {
		return
	}
	tail := lines[ccpmHeaderLines:]
	if len(tail) > ccpmMaxLines {
		tail = tail[len(tail)-ccpmMaxLines:]
	}
	kept := append([]string{}, lines[:ccpmHeaderLines]...)
	kept = append(kept, tail...)
	out := strings.Join(kept, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(out), 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
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
