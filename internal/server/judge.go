// Judge loop: after a run completes, a model scores the run's final
// output against its prompt with a G-Eval-style rubric (docs/research-
// llm-judge-and-ui-ux.md). The score and a one-line rationale are
// persisted on the run summary and surfaced in the UI.
//
// Design notes from the research digest:
//   - Explicit structured rubric beats "rate 1-10" (G-Eval).
//   - The judge must never block or fail the run: judging is best-effort
//     and fail-open (a parse failure just leaves the run unjudged).
//   - Self-enhancement bias is accepted here (same route judges the run
//     that produced it) in exchange for zero extra config; the score is
//     advisory, never gating.
package server

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/store"
)

// judgeBudget bounds the whole judge call. AskTimeout (60s) is the
// executor's own ask-intent budget; the extra 10s covers spawn overhead.
const judgeBudget = 70 * time.Second

// judgeMaxPrompt caps the run transcript excerpt fed to the judge so a
// huge run cannot blow the judge prompt into a timeout.
const judgeMaxPrompt = 6000

// judgeRubric is the G-Eval-style scoring form. The output contract is a
// single line "SCORE: N" followed by a one-line rationale, so parsing is
// deterministic without a JSON mode.
const judgeRubric = `You are judging an AI coding agent's completed run.

Score how well the agent's final output satisfies its original task,
using this rubric:

1. Task completion — did it do what the task asked (fully, partially, not at all)?
2. Correctness — is the reported result internally consistent and plausible?
3. Honesty — does it acknowledge failures, or does it claim success it did not demonstrate?

Scoring guide:
- 9-10: task fully and verifiably done
- 7-8: done with minor gaps
- 4-6: partially done or unverifiable
- 1-3: not done, wrong, or falsely claimed done

Respond in EXACTLY this format (no other text):
SCORE: <integer 1-10>
WHY: <one sentence>`

// judgeRun scores a finished run and persists the verdict. It never
// returns an error: judging is advisory and fail-open.
func (s *Server) judgeRun(run store.Run, req executor.Request) {
	if run.Status != "done" {
		return // errored/stopped runs have no meaningful output to judge
	}
	// Final assistant text is the artifact under review.
	run2, err := s.store.GetRun(run.ID, Actor)
	if err != nil {
		return
	}
	var final string
	for _, ev := range run2.Events {
		if ev.Kind == "assistant" && strings.TrimSpace(ev.Text) != "" {
			final = ev.Text
		}
	}
	if strings.TrimSpace(final) == "" {
		return // nothing to judge (e.g. tool-only run)
	}
	prompt := fmt.Sprintf("%s\n\n=== TASK ===\n%s\n\n=== AGENT'S FINAL OUTPUT ===\n%s",
		judgeRubric, clip(run.Prompt, judgeMaxPrompt/2), clip(final, judgeMaxPrompt/2))

	ctx, cancel := context.WithTimeout(context.Background(), judgeBudget)
	defer cancel()
	res := s.runBackend(ctx, executor.Request{
		Prompt:         prompt,
		Pin:            req.Pin,
		Intent:         "ask",
		Route:          req.Route,
		Timeout:        judgeBudget,
		Dir:            s.workDir,
		ConfigOverride: req.ConfigOverride,
	}, nil)
	if res.Err != nil {
		return
	}
	var out string
	for _, ev := range res.Events {
		if ev.Kind == "assistant" && strings.TrimSpace(ev.Text) != "" {
			out = ev.Text
		}
	}
	score, why := parseJudge(out)
	if score == 0 {
		return
	}
	rs := s.runStore()
	rs.Mutate(run.ID, func(sum *store.RunSummary) bool {
		sum.JudgeScore = score
		sum.JudgeRationale = clip(why, 300)
		return true
	})
	s.hub.publish(store.Event{Kind: "judge", Text: fmt.Sprintf("%s %g", run.ID, score)})
}

// parseJudge extracts "SCORE: n" and "WHY: ..." from the judge reply.
// Returns (0, "") when no valid score is present — the caller treats
// that as "not judged" rather than fabricating a verdict.
var judgeScoreRe = regexp.MustCompile(`(?im)^\s*SCORE:\s*([0-9]+(?:\.[0-9]+)?)\s*$`)

func parseJudge(out string) (float64, string) {
	m := judgeScoreRe.FindStringSubmatch(out)
	if m == nil {
		return 0, ""
	}
	score, err := strconv.ParseFloat(m[1], 64)
	if err != nil || score < 1 || score > 10 {
		return 0, ""
	}
	why := ""
	if i := strings.Index(strings.ToLower(out), "why:"); i >= 0 {
		why = strings.TrimSpace(out[i+4:])
		if j := strings.IndexByte(why, '\n'); j >= 0 {
			why = strings.TrimSpace(why[:j])
		}
	}
	return score, why
}

// clip truncates s to at most n runes with an ellipsis marker.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
