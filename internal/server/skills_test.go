package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/skills"
	"github.com/florianleibert/flobot/internal/store"
)

// newSkillServer returns a server backed by a real store on a temp state file,
// plus the argv capture path the fake binary writes.
func newSkillServer(t *testing.T) (*httptest.Server, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	argvPath := filepath.Join(dir, "argv.txt")
	ex := &executor.Exec{
		Bin:     bin,
		Environ: func() []string { return []string{"FLOCODE_FAKE_ARGV=" + argvPath} },
	}
	st := store.New()
	st.SetPersistPath(filepath.Join(dir, "state.json"))
	srv := New(st, WithExec(ex), WithWorkDir(dir))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, st, argvPath
}

// M2 acceptance: the prompt for a bot with five skills carries five one-line
// index entries and zero procedure bodies.
func TestSkillIndexInPromptHasNoBodies(t *testing.T) {
	ts, _, argvPath := newSkillServer(t)

	names := []string{"deploy", "audit", "migrate", "review", "triage"}
	for _, name := range names {
		st := postJSONStatus(t, ts, "/api/v1/skills/contract", map[string]any{
			"name":         name,
			"when_to_use":  "when doing " + name,
			"procedure":    "BODY-MARKER-" + name,
			"verification": "checked " + name,
		})
		if st.code != http.StatusOK {
			t.Fatalf("create skill %s status = %d: %s", name, st.code, st.body)
		}
	}

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "M", "role": "r", "route": "glm"})
	run := postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": bot["id"], "prompt": "do the thing", "intent": "ask",
	})
	waitEvents(t, ts, run["id"].(string))

	argv := readText(t, argvPath)
	for _, name := range names {
		if !strings.Contains(argv, name) {
			t.Fatalf("skill index missing %q:\n%s", name, argv)
		}
	}
	for _, name := range names {
		if strings.Contains(argv, "BODY-MARKER-"+name) {
			t.Fatalf("prompt leaked the %q procedure body:\n%s", name, argv)
		}
	}
}

// M2 acceptance: a drafted skill is inert until a person approves it.
func TestDraftedSkillIsInertUntilApproved(t *testing.T) {
	ts, st, _ := newSkillServer(t)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "M", "role": "r", "route": "glm"})
	run := postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": bot["id"], "prompt": "deploy the thing", "intent": "ask",
	})
	waitEvents(t, ts, run["id"].(string))

	res := postJSONStatus(t, ts, "/api/v1/runs/"+run["id"].(string)+"/skill-draft", map[string]any{})
	if res.code != http.StatusCreated {
		t.Fatalf("draft status = %d: %s", res.code, res.body)
	}
	var draft struct {
		Skill store.Skill `json:"skill"`
		Live  bool        `json:"live"`
	}
	if err := json.Unmarshal([]byte(res.body), &draft); err != nil {
		t.Fatalf("draft body: %s", res.body)
	}
	if draft.Skill.Status != skills.StatusDraft {
		t.Fatalf("status = %q, want draft", draft.Skill.Status)
	}
	if draft.Live {
		t.Fatal("a fresh draft reported itself live")
	}
	if draft.Skill.DraftedFrom != run["id"] {
		t.Fatalf("DraftedFrom = %q, want %v", draft.Skill.DraftedFrom, run["id"])
	}

	// The draft must not reach the prompt index.
	if idx := st.SkillIndex(store.ActorLocal); strings.Contains(idx, draft.Skill.Name) {
		t.Fatalf("draft appeared in the skill index: %q", idx)
	}

	if res := postJSONStatus(t, ts, "/api/v1/skills/"+draft.Skill.ID+"/approve", map[string]any{}); res.code != http.StatusOK {
		t.Fatalf("approve status = %d: %s", res.code, res.body)
	}
	if idx := st.SkillIndex(store.ActorLocal); !strings.Contains(idx, draft.Skill.Name) {
		t.Fatalf("approved skill missing from the index: %q", idx)
	}
}

// A draft cannot render a prompt body or back a routine.
func TestDraftSkillHasNoPromptBody(t *testing.T) {
	st := store.New()
	st.SetPersistPath(filepath.Join(t.TempDir(), "state.json"))
	draft, err := st.DraftSkillFromRun(store.ActorLocal, "run-1", skills.Contract{
		Name:      "learned",
		WhenToUse: "when learning",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SkillPromptBody(draft.ID, store.ActorLocal); err == nil {
		t.Fatal("a draft must not render a prompt body")
	}
	if _, err := st.ApproveSkill(draft.ID, store.ActorLocal); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SkillPromptBody(draft.ID, store.ActorLocal); err != nil {
		t.Fatalf("approved skill must render: %v", err)
	}
}

func TestSkillContractSurface(t *testing.T) {
	ts, _, _ := newSkillServer(t)

	created := postJSON(t, ts, "/api/v1/skills/contract", map[string]any{
		"name":         "deploy",
		"when_to_use":  "when shipping",
		"procedure":    "build then push",
		"pitfalls":     "do not skip the tag",
		"verification": "tag exists on remote",
	})
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("create returned no id: %v", created)
	}

	got := getJSON(t, ts, "/api/v1/skills/"+id)
	if got["live"] != true {
		t.Fatalf("created skill not live: %v", got["live"])
	}
	if rendered, _ := got["rendered"].(string); !strings.Contains(rendered, "build then push") {
		t.Fatalf("rendered = %q", rendered)
	}

	if res := patchJSONStatus(t, ts, "/api/v1/skills/"+id, map[string]any{"procedure": "build, test, push"}); res.code != http.StatusOK {
		t.Fatalf("patch status = %d: %s", res.code, res.body)
	}
	after := getJSON(t, ts, "/api/v1/skills/"+id)
	if rendered, _ := after["rendered"].(string); !strings.Contains(rendered, "build, test, push") {
		t.Fatalf("patch not applied: %q", rendered)
	}
}

// A skill with no when-to-use but a body still works (legacy shape).
func TestLegacySkillStillIndexed(t *testing.T) {
	st := store.New()
	st.SetPersistPath(filepath.Join(t.TempDir(), "state.json"))
	if _, err := st.CreateSkill(store.ActorLocal, "reviewer", "review the diff"); err != nil {
		t.Fatal(err)
	}
	if idx := st.SkillIndex(store.ActorLocal); !strings.Contains(idx, "reviewer") {
		t.Fatalf("legacy skill missing from index: %q", idx)
	}
}

// readText reads a file the fake binary wrote.
func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
