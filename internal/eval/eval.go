// Package eval measures runs instead of guessing about them.
//
// Three things were missing and all three are cheap:
//
//   - The routing decision was never recorded. A run carried a pin and
//     nothing said why that pin won or what it fell back from, so "why did this
//     run use Qwen" had no answer after the fact.
//   - Finished runs were never scored. The events were kept and then never read,
//     so a regression looked exactly like a good day.
//   - An operator's judgement of a run was thrown away. Saying "that answer was
//     bad" changed nothing and was forgotten on restart.
//
// Nothing here calls a model. Every scorer is deterministic over stored events,
// so replaying ten runs twice gives the same ten answers.
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Event is the minimal event shape a scorer reads. It mirrors the stored event
// without importing the store, so this package stays a leaf.
type Event struct {
	Kind    string `json:"kind"`
	TurnID  uint64 `json:"turn_id,omitempty"`
	Text    string `json:"text,omitempty"`
	Tool    string `json:"tool,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
}

// Run is one recorded run as the evaluator sees it.
type Run struct {
	ID     string  `json:"id"`
	BotID  string  `json:"bot_id,omitempty"`
	Route  string  `json:"route,omitempty"`
	Status string  `json:"status"`
	Prompt string  `json:"prompt"`
	Events []Event `json:"events"`
}

// Decision records why a run used the route it did.
//
// It is written before dispatch and kept with the run: an unexplained route is
// indistinguishable from a bug six weeks later.
type Decision struct {
	Route     string   `json:"route"`
	Model     string   `json:"model,omitempty"`
	Reason    string   `json:"reason"`
	Fallbacks []string `json:"fallbacks,omitempty"`
	// Filtered names the routes capacity filtering removed, so an empty
	// fallback chain is explained rather than mysterious.
	Filtered []string `json:"filtered,omitempty"`
}

// String renders the decision as one line for a run summary or a log.
func (d Decision) String() string {
	var b strings.Builder
	b.WriteString(d.Route)
	if d.Model != "" {
		b.WriteString(" (" + d.Model + ")")
	}
	if d.Reason != "" {
		b.WriteString(": " + d.Reason)
	}
	if len(d.Fallbacks) > 0 {
		b.WriteString(" · fallback " + strings.Join(d.Fallbacks, " → "))
	}
	if len(d.Filtered) > 0 {
		b.WriteString(" · filtered " + strings.Join(d.Filtered, ","))
	}
	return b.String()
}

// Score is one scorer's verdict on one run.
type Score struct {
	Scorer string `json:"scorer"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}

// Scorer is a deterministic check over a run's stored events.
type Scorer struct {
	Name  string
	Check func(Run) (bool, string)
}

// DefaultScorers are the checks that hold for every run regardless of intent.
//
// They are deliberately about shape, not quality: "did it finish", "did it say
// anything", "did it leave an error behind". Quality needs a human verdict,
// which is what Verdicts is for.
func DefaultScorers() []Scorer {
	return []Scorer{
		{
			Name: "has_output",
			Check: func(r Run) (bool, string) {
				for _, ev := range r.Events {
					if (ev.Kind == "content" || ev.Kind == "delta") && strings.TrimSpace(ev.Text) != "" {
						return true, ""
					}
				}
				return false, "no content or delta event carried text"
			},
		},
		{
			Name: "no_error_events",
			Check: func(r Run) (bool, string) {
				var errs []string
				for _, ev := range r.Events {
					if ev.Kind == "err" || ev.IsError {
						errs = append(errs, strings.TrimSpace(ev.Text))
					}
				}
				if len(errs) == 0 {
					return true, ""
				}
				return false, strings.Join(errs, "; ")
			},
		},
		{
			Name: "status_matches_events",
			Check: func(r Run) (bool, string) {
				want := DeriveStatus(r.Events)
				if r.Status == "" || want == r.Status {
					return true, ""
				}
				return false, fmt.Sprintf("recorded %q but events say %q", r.Status, want)
			},
		},
	}
}

// DeriveStatus infers what a run's outcome should be from its events alone.
//
// This is the replay oracle: if re-deriving the status from stored events does
// not reproduce what was recorded at the time, either the recording or the
// event stream changed underneath us.
func DeriveStatus(events []Event) string {
	sawDone := false
	for _, ev := range events {
		switch ev.Kind {
		case "done":
			sawDone = true
		case "err":
			return "error"
		}
		if ev.IsError {
			return "error"
		}
	}
	if sawDone {
		return "done"
	}
	return ""
}

// ReplayResult is one run's replay outcome.
type ReplayResult struct {
	RunID      string  `json:"run_id"`
	Status     string  `json:"status"`
	Scores     []Score `json:"scores"`
	Reproduced bool    `json:"reproduced"`
}

// Replay scores every run and reports whether its recorded outcome was
// reproduced from the stored events.
//
// Reproduced is stricter than "all scorers passed": it means the status derived
// from events equals the status recorded at run time. A run whose scorers pass
// but whose status disagrees is exactly the drift worth catching.
func Replay(runs []Run) []ReplayResult {
	return ReplayWith(runs, DefaultScorers())
}

// ReplayWith replays with an explicit scorer set.
func ReplayWith(runs []Run, scorers []Scorer) []ReplayResult {
	out := make([]ReplayResult, 0, len(runs))
	for _, r := range runs {
		res := ReplayResult{RunID: r.ID, Status: r.Status}
		for _, sc := range scorers {
			pass, detail := sc.Check(r)
			res.Scores = append(res.Scores, Score{Scorer: sc.Name, Pass: pass, Detail: detail})
		}
		res.Reproduced = r.Status == "" || DeriveStatus(r.Events) == r.Status
		out = append(out, res)
	}
	return out
}

// Summary counts how many replayed runs reproduced their recorded outcome.
func Summary(results []ReplayResult) (reproduced, total int) {
	total = len(results)
	for _, r := range results {
		if r.Reproduced {
			reproduced++
		}
	}
	return reproduced, total
}

// Verdict is an operator's judgement of one run. It is kept because a thrown-away
// label cannot train anything later.
type Verdict struct {
	RunID   string `json:"run_id"`
	Verdict string `json:"verdict"` // "good" or "bad"
	Note    string `json:"note,omitempty"`
}

const (
	VerdictGood = "good"
	VerdictBad  = "bad"
)

// Verdicts is a persisted run-id → verdict map.
//
// It is a plain JSON file under ~/.flobot like every other store here: no schema,
// so no migration. A missing file is an empty set of verdicts, not an error.
type Verdicts struct {
	mu   sync.Mutex
	path string
	m    map[string]Verdict
}

// NewVerdicts returns an empty verdict set bound to path. Call Load to read an
// existing file.
func NewVerdicts(path string) *Verdicts {
	return &Verdicts{path: path, m: map[string]Verdict{}}
}

// SetPath rebinds the store to path and clears what was loaded.
func (v *Verdicts) SetPath(path string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.path = path
	v.m = map[string]Verdict{}
}

// Load reads the verdict file. A missing file leaves the set empty.
func (v *Verdicts) Load() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.path == "" {
		return nil
	}
	b, err := os.ReadFile(v.path)
	if err != nil {
		if os.IsNotExist(err) {
			v.m = map[string]Verdict{}
			return nil
		}
		return err
	}
	var rows []Verdict
	if err := json.Unmarshal(b, &rows); err != nil {
		return fmt.Errorf("eval verdicts %s: %w", v.path, err)
	}
	m := make(map[string]Verdict, len(rows))
	for _, row := range rows {
		if row.RunID == "" || !ValidVerdict(row.Verdict) {
			continue
		}
		m[row.RunID] = row
	}
	v.m = m
	return nil
}

// ValidVerdict reports whether s is an accepted verdict value.
func ValidVerdict(s string) bool { return s == VerdictGood || s == VerdictBad }

// Record stores an operator verdict and persists it. An invalid verdict is an
// error rather than a silent no-op: a label that did not stick must be visible.
func (v *Verdicts) Record(runID, verdict, note string) error {
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("eval: run id required")
	}
	if !ValidVerdict(verdict) {
		return fmt.Errorf("eval: verdict must be %q or %q", VerdictGood, VerdictBad)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.m == nil {
		v.m = map[string]Verdict{}
	}
	v.m[runID] = Verdict{RunID: runID, Verdict: verdict, Note: strings.TrimSpace(note)}
	return v.saveLocked()
}

// Get returns the verdict for a run.
func (v *Verdicts) Get(runID string) (Verdict, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v2, ok := v.m[runID]
	return v2, ok
}

// All returns every verdict sorted by run id.
func (v *Verdicts) All() []Verdict {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.allLocked()
}

// allLocked is All for a caller that already holds the lock.
func (v *Verdicts) allLocked() []Verdict {
	out := make([]Verdict, 0, len(v.m))
	for _, row := range v.m {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RunID < out[j].RunID })
	return out
}

func (v *Verdicts) saveLocked() error {
	if v.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(v.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v.allLocked(), "", "  ")
	if err != nil {
		return err
	}
	tmp := v.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, v.path)
}

// BotContext is what prompt assembly knows about a bot before any request.
//
// Everything in it is stable across runs of the same bot, which is what makes it
// usable as a cacheable prefix on local MLX routes.
type BotContext struct {
	Name      string   `json:"name"`
	Role      string   `json:"role"`
	Memory    string   `json:"memory,omitempty"`
	Soul      string   `json:"soul,omitempty"`
	SysPrompt string   `json:"sys_prompt,omitempty"`
	Sessions  []string `json:"sessions,omitempty"`
}

// Request is the per-run part of a prompt.
type Request struct {
	Prompt    string `json:"prompt"`
	Intent    string `json:"intent"`
	SkillBody string `json:"skill_body,omitempty"`
}

// PromptParts is an assembled prompt split into a stable prefix and a volatile
// tail.
//
// The split exists because a local MLX route reuses its prompt cache only while
// the prefix is byte-identical. Rebuilding memory-first per run — which is what
// happened before — invalidated the cache on every single run.
type PromptParts struct {
	Prefix []string `json:"prefix"`
	Tail   []string `json:"tail"`
}

// Assemble builds a prompt with every stable section first and everything that
// varies per run last.
func Assemble(bot BotContext, req Request) PromptParts {
	var prefix []string
	if s := strings.TrimSpace(bot.SysPrompt); s != "" {
		prefix = append(prefix, s)
	}
	if s := strings.TrimSpace(bot.Soul); s != "" {
		prefix = append(prefix, s)
	}
	if s := strings.TrimSpace(bot.Name); s != "" || strings.TrimSpace(bot.Role) != "" {
		id := strings.TrimSpace(bot.Name)
		if r := strings.TrimSpace(bot.Role); r != "" {
			id += " (" + r + ")"
		}
		prefix = append(prefix, "You are "+strings.TrimSpace(id)+".")
	}
	if s := strings.TrimSpace(bot.Memory); s != "" {
		prefix = append(prefix, "Memory:\n"+s)
	}
	for _, s := range bot.Sessions {
		if s = strings.TrimSpace(s); s != "" {
			prefix = append(prefix, s)
		}
	}

	var tail []string
	if s := strings.TrimSpace(req.SkillBody); s != "" {
		tail = append(tail, "Skill (follow this how-to):\n"+s)
	}
	if s := strings.TrimSpace(req.Intent); s != "" {
		tail = append(tail, "Intent: "+s)
	}
	tail = append(tail, strings.TrimSpace(req.Prompt))
	return PromptParts{Prefix: prefix, Tail: tail}
}

// String joins the parts into one prompt.
func (p PromptParts) String() string {
	var b strings.Builder
	for _, s := range p.Prefix {
		b.WriteString(s)
		b.WriteString("\n\n")
	}
	for i, s := range p.Tail {
		b.WriteString(s)
		if i < len(p.Tail)-1 {
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

// PrefixString is just the stable prefix. Two runs of the same bot must produce
// identical PrefixString or the local prompt cache is wasted.
func (p PromptParts) PrefixString() string { return strings.Join(p.Prefix, "\n\n") }
