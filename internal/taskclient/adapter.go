// Adapter: run a flobot executor.Request over the flocode task API,
// translating SSE events into executor events. Selected by
// FLOBOT_BACKEND=taskclient; the child-process executor stays default.
package taskclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/florianleibert/flobot/internal/executor"
)

// Adapter runs executor requests over the task API.
type Adapter struct {
	Client *Client
	// Fallback is used when the task API is unreachable at submit time.
	Fallback *executor.Exec
}

// taskBody is the task.Task JSON object posted to /api/v1/tasks.
type taskBody struct {
	Goal       string `json:"goal"`
	Dir        string `json:"dir,omitempty"`
	Mode       string `json:"mode,omitempty"`
	MaxTurns   int    `json:"max_turns,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

// RunResult is the adapter's result: the executor result plus the
// task-API result contract fields.
type RunResult struct {
	executor.Result
	Contract *ResultContract
}

// ResultContract is the flocode task result envelope subset flobot
// surfaces in the UI (confidence, tests, review flag).
type ResultContract struct {
	Confidence         float64
	RequiresHumanReview bool
	TestsPassed        int
	TestsTotal        int
	Summary           string
}

// Run implements the executor surface used by server.spawn.
func (a *Adapter) Run(ctx context.Context, req executor.Request, emit func(ev executor.Event)) executor.Result {
	body := taskBody{
		Goal: strings.TrimSpace(req.SkillBody + "\n\n" + req.Prompt),
		Dir:  req.Dir,
	}
	if req.Worktree != "" && body.Dir == "" {
		body.Dir = req.Worktree
	}
	if req.Timeout > 0 {
		body.TimeoutSec = int(req.Timeout.Seconds())
	}
	sub, err := a.Client.Submit(ctx, body)
	if err != nil {
		if a.Fallback != nil {
			// Log once: a silent fallback turns a config error into
			// every run quietly using the child executor.
			log.Printf("flobot: taskclient submit failed (%v); falling back to child executor", err)
			return a.Fallback.Run(ctx, req, emit)
		}
		return executor.Result{Err: fmt.Errorf("taskclient: submit: %w", err)}
	}
	// Stream events until terminal, then fetch the result.
	evCh, err := a.Client.Stream(ctx, sub.TaskID)
	if err != nil {
		// Stream failed: poll status to terminal instead.
		return a.pollToTerminal(ctx, sub.TaskID, emit)
	}
	for ev := range evCh {
		if ctx.Err() != nil {
			_ = a.Client.Cancel(context.Background(), sub.TaskID)
			return executor.Result{Err: ctx.Err()}
		}
		kind := ev.Type
		switch ev.Type {
		case "tool_call":
			kind = "tool_use"
		case "tool_result":
			kind = "tool_result"
		case "completed":
			kind = "assistant"
		case "failed":
			kind = "error"
		}
		text := ""
		if len(ev.Data) > 0 {
			var m map[string]any
			if json.Unmarshal(ev.Data, &m) == nil {
				if t, ok := m["text"].(string); ok {
					text = t
				} else if t, ok := m["summary"].(string); ok {
					text = t
				}
			}
		}
		emit(executor.Event{Kind: kind, Text: text})
	}
	if ctx.Err() != nil {
		_ = a.Client.Cancel(context.Background(), sub.TaskID)
		return executor.Result{Err: ctx.Err()}
	}
	// The stream may have died mid-run (network blip) while the task
	// is still executing server-side: poll to terminal instead of
	// erroring out while the workspace keeps mutating. A Status error
	// here is also transient — routing to Result would error the run
	// while the task keeps writing the workspace.
	if st, err := a.Client.Status(ctx, sub.TaskID); err != nil || !Terminal(st.Status) {
		return a.pollToTerminal(ctx, sub.TaskID, emit)
	}
	res, err := a.Client.Result(ctx, sub.TaskID)
	if err != nil {
		return executor.Result{Err: fmt.Errorf("taskclient: result: %w", err)}
	}
	if res.Status == "failed" || res.Error != "" {
		return executor.Result{Err: fmt.Errorf("task failed: %s", res.Error)}
	}
	emit(executor.Event{Kind: "assistant", Text: res.Summary})
	// Surface the result contract as a final event so the UI can render
	// chips (confidence, tests, review flag) without a second fetch.
	// Tests arrive as a raw JSON object {passed, total} or similar.
	passed, total := parseTests(res.Tests)
	if payload, err := json.Marshal(map[string]any{
		"confidence":            res.Confidence,
		"requires_human_review": res.RequiresHumanReview,
		"summary":              res.Summary,
		"tests_passed":         passed,
		"tests_total":          total,
		"patch_sha256":         res.Patch.SHA256,
		"patch_changed_files":  res.Patch.ChangedFiles,
	}); err == nil {
		emit(executor.Event{Kind: "result_contract", Text: string(payload)})
	}
	return executor.Result{}
}

// parseTests extracts passed/total counts from the raw tests envelope.
func parseTests(raw json.RawMessage) (passed, total int) {
	if len(raw) == 0 {
		return 0, 0
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return 0, 0
	}
	if v, ok := m["passed"].(float64); ok {
		passed = int(v)
	}
	if v, ok := m["total"].(float64); ok {
		total = int(v)
	}
	return passed, total
}

// pollToTerminal polls status when streaming is unavailable. It
// emits the same result_contract event as the stream path so the UI
// contract does not differ by transport accident.
func (a *Adapter) pollToTerminal(ctx context.Context, id string, emit func(ev executor.Event)) executor.Result {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	// A permanently-down task API must not poll forever: after a
	// budget of consecutive Status errors, fail the run. A wedged
	// task that never reaches terminal (and never errors Status) is
	// bounded by a hard wall-clock deadline too.
	const maxStatusErrs = 20
	const pollDeadline = 30 * time.Minute
	deadline := time.After(pollDeadline)
	errBudget := maxStatusErrs
	for {
		select {
		case <-ctx.Done():
			_ = a.Client.Cancel(context.Background(), id)
			return executor.Result{Err: ctx.Err()}
		case <-deadline:
			_ = a.Client.Cancel(context.Background(), id)
			return executor.Result{Err: fmt.Errorf("taskclient: poll deadline (%s) exceeded for task %s", pollDeadline, id)}
		case <-t.C:
			st, err := a.Client.Status(ctx, id)
			if err != nil {
				errBudget--
				if errBudget <= 0 {
					return executor.Result{Err: fmt.Errorf("taskclient: status: %w", err)}
				}
				continue
			}
			errBudget = maxStatusErrs
			if Terminal(st.Status) {
				res, err := a.Client.Result(ctx, id)
				if err != nil {
					return executor.Result{Err: err}
				}
				if res.Status == "failed" || res.Error != "" {
					return executor.Result{Err: fmt.Errorf("task failed: %s", res.Error)}
				}
				emit(executor.Event{Kind: "assistant", Text: res.Summary})
				passed, total := parseTests(res.Tests)
				if payload, err := json.Marshal(map[string]any{
					"confidence":            res.Confidence,
					"requires_human_review": res.RequiresHumanReview,
					"summary":              res.Summary,
					"tests_passed":         passed,
					"tests_total":          total,
					"patch_sha256":         res.Patch.SHA256,
					"patch_changed_files":  res.Patch.ChangedFiles,
				}); err == nil {
					emit(executor.Event{Kind: "result_contract", Text: string(payload)})
				}
				return executor.Result{}
			}
		}
	}
}
