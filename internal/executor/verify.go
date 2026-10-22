// verify.go implements the Muse Spark 1.2 verification run. It spawns
// flocode with the muse pin, injects the Muse API key into the child
// environment as MODEL_API_KEY (and only there), and emits a `verify`
// NDJSON event on success. On a missing or invalid key it emits an `err`
// event with an actionable message that never discloses key material.
package executor

import (
	"context"
	"errors"
	"os"
	"strings"
)

// MusePin is the flocode --model pin for Muse Spark 1.3.
const MusePin = "muse-spark-1.3"

// MuseRoute is the route id that maps to Muse Spark 1.2.
const MuseRoute = "muse"

// VerifyPrompt is the prompt sent to flocode for a verification run.
const VerifyPrompt = "Verify the Muse Spark 1.3 integration: confirm the model API key is valid and the model responds, then report the result."

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
	res := e.run(ctx, req, env, func(ev Event) {
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

// IsVerifyError reports whether err is a missing/invalid Muse API key.
func IsVerifyError(err error) bool {
	return errors.Is(err, ErrModelAPIKeyMissing)
}
