// verify.go implements the Muse Spark 1.2 verification run. It spawns
// flocode with the muse pin, injects the Muse API key into the child
// environment as MODEL_API_KEY (and only there), and emits a `verify`
// NDJSON event on success. On a missing or invalid key it emits an `err`
// event with an actionable message that never discloses key material.
package executor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// MusePin is the flocode --model pin for Muse Spark 1.2.
const MusePin = "muse-spark-1.2"

// MuseRoute is the route id that maps to Muse Spark 1.2.
const MuseRoute = "muse"

// VerifyPrompt is the prompt sent to flocode for a verification run.
const VerifyPrompt = "Verify the Muse Spark 1.2 integration: confirm the model API key is valid and the model responds, then report the result."

// Verify runs a Muse Spark 1.2 verification through flocode.
//
// The Muse API key is read at runtime from <home>/.accounts/meta.ai and
// exposed to the child process only as the MODEL_API_KEY environment
// variable. It is never logged, printed, or persisted.
//
// On success the emitted events end with a `verify` event (is_error false).
// On a missing or invalid key the emitted events end with an `err` event
// whose text is an actionable message that names the key file but never
// any of its contents.
func (e *Exec) Verify(ctx context.Context, req Request, emit func(Event)) Result {
	if emit == nil {
		emit = func(Event) {}
	}

	// Resolve the key up front so a missing/invalid key produces an
	// actionable `err` event without spawning a child that would fail
	// with an opaque model error.
	key, err := ReadModelAPIKey(e.home())
	if err != nil {
		msg := "verify failed: " + err.Error()
		ev := Event{Kind: "err", Text: msg}
		emit(ev)
		return Result{Events: []Event{ev}, Err: err}
	}

	// Inject the key into the child environment only. The base env is
	// copied; the key never appears in argv, events, or logs.
	base := e.baseEnv()
	env := WithModelAPIKey(base, key)

	if strings.TrimSpace(req.Prompt) == "" {
		req.Prompt = VerifyPrompt
	}
	if strings.TrimSpace(req.Pin) == "" {
		req.Pin = MusePin
	}
	if strings.TrimSpace(req.Intent) == "" {
		req.Intent = "ask"
	}
	if strings.TrimSpace(req.Route) == "" {
		req.Route = MuseRoute
	}

	var events []Event
	res := e.runWithEnv(ctx, req, env, func(ev Event) {
		events = append(events, ev)
		emit(ev)
	})

	// Success: the child exited cleanly and did not report an error.
	if res.Err == nil {
		ok := false
		ev := Event{Kind: "verify", Text: "Muse Spark 1.2 verification passed", IsError: &ok}
		events = append(events, ev)
		emit(ev)
		res.Events = events
		return res
	}

	// Failure: emit an `err` event with an actionable message. If the
	// child already emitted a terminal `err` (e.g. the model rejected the
	// key), keep it; otherwise add one.
	if len(events) == 0 || events[len(events)-1].Kind != "err" {
		msg := "verify failed: " + res.Err.Error() +
			" — check that ~/.accounts/meta.ai contains a valid Muse API key"
		ev := Event{Kind: "err", Text: msg}
		events = append(events, ev)
		emit(ev)
	} else {
		// The child emitted a terminal `err` (e.g. the model rejected the
		// key). Make it actionable without disclosing key material.
		if !strings.Contains(events[len(events)-1].Text, "meta.ai") {
			events[len(events)-1].Text +=
				" — check that ~/.accounts/meta.ai contains a valid Muse API key"
			emit(events[len(events)-1])
		}
	}
	res.Events = events
	return res
}

// baseEnv returns the base child environment (no key).
func (e *Exec) baseEnv() []string {
	if e != nil && e.Environ != nil {
		return e.Environ()
	}
	return os.Environ()
}

// runWithEnv is like Run but uses a pre-built child environment, so the
// caller controls exactly which variables (including MODEL_API_KEY) the
// child sees.
func (e *Exec) runWithEnv(ctx context.Context, req Request, env []string, emit func(Event)) Result {
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
	cmd.Env = env
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

// IsVerifyError reports whether err is a missing/invalid Muse API key.
func IsVerifyError(err error) bool {
	return errors.Is(err, ErrModelAPIKeyMissing)
}
