package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/florianleibert/flobot/internal/store"
)

// deleteJSON issues a DELETE and returns the decoded body, failing on a
// non-2xx status. The existing helpers cover GET/POST/PATCH; the memory
// surface is the first to need DELETE with a body.
func deleteJSON(t *testing.T, ts *httptest.Server, path string) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, ts.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		t.Fatalf("%s -> %d %s", path, resp.StatusCode, b)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%s: %s", path, b)
	}
	return out
}

// getJSONStatus issues a GET and returns the status without failing on a
// non-2xx, for the negative cases.
func getJSONStatus(t *testing.T, ts *httptest.Server, path string) struct {
	code int
	body string
} {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return struct {
		code int
		body string
	}{resp.StatusCode, string(b)}
}

// newMemoryServer is the minimal harness for the memory surface: a real store
// on a temp state file, no executor needed.
func newMemoryServer(t *testing.T) (*httptest.Server, store.Bot) {
	t.Helper()
	st := store.New()
	st.SetPersistPath(t.TempDir() + "/state.json")
	srv := New(st)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	bot, err := st.CreateBot(store.ActorLocal, "M", "coder", "glm")
	if err != nil {
		t.Fatal(err)
	}
	return ts, bot
}

func TestBotMemorySurface(t *testing.T) {
	ts, bot := newMemoryServer(t)

	got := postJSON(t, ts, "/api/v1/bots/"+bot.ID+"/memory", map[string]any{"text": "always write tests first"})
	if got["memory"] == "" {
		t.Fatalf("add returned no memory: %+v", got)
	}
	if used, _ := got["memory_used"].(float64); used == 0 {
		t.Fatalf("add returned no usage: %+v", got)
	}

	view := getJSON(t, ts, "/api/v1/bots/"+bot.ID+"/memory")
	facts, _ := view["facts"].([]any)
	if len(facts) != 1 || facts[0] != "always write tests first" {
		t.Fatalf("facts = %v", view["facts"])
	}
	if cap, _ := view["cap"].(float64); cap == 0 {
		t.Fatalf("cap = %v", view["cap"])
	}

	// Patch fact 0.
	patched := patchJSON(t, ts, "/api/v1/bots/"+bot.ID+"/memory", map[string]any{"index": 0, "text": "TDD always"})
	if !strings.Contains(patched["memory"].(string), "TDD always") {
		t.Fatalf("patch = %+v", patched)
	}

	// Remove fact 0.
	removed := deleteJSON(t, ts, "/api/v1/bots/"+bot.ID+"/memory/0")
	if mem, _ := removed["memory"].(string); mem != "" {
		t.Fatalf("after remove memory = %q", mem)
	}
}

// A full memory is a 409 carrying the usage, so the client can offer to remove
// something instead of losing the fact silently.
func TestBotMemoryFullIsConflict(t *testing.T) {
	ts, bot := newMemoryServer(t)
	if st := postJSONStatus(t, ts, "/api/v1/bots/"+bot.ID+"/memory/cap", map[string]any{"cap": 64}); st.code != 200 {
		t.Fatalf("set cap status = %d: %s", st.code, st.body)
	}
	if st := postJSONStatus(t, ts, "/api/v1/bots/"+bot.ID+"/memory", map[string]any{"text": strings.Repeat("a", 40)}); st.code != 200 {
		t.Fatalf("first fact status = %d: %s", st.code, st.body)
	}
	st := postJSONStatus(t, ts, "/api/v1/bots/"+bot.ID+"/memory", map[string]any{"text": strings.Repeat("b", 40)})
	if st.code != http.StatusConflict {
		t.Fatalf("full memory status = %d, want 409: %s", st.code, st.body)
	}
	if !strings.Contains(st.body, "of 64 bytes") {
		t.Fatalf("conflict body must name the usage: %s", st.body)
	}
}

func TestBotWorkLogSurface(t *testing.T) {
	ts, bot := newMemoryServer(t)
	if st := postJSONStatus(t, ts, "/api/v1/bots/"+bot.ID+"/log", map[string]any{"summary": "shipped M1", "run_id": "run-7"}); st.code != 200 {
		t.Fatalf("log status = %d: %s", st.code, st.body)
	}
	view := getJSON(t, ts, "/api/v1/bots/"+bot.ID+"/memory")
	log, _ := view["log"].([]any)
	if len(log) != 1 {
		t.Fatalf("log = %v", view["log"])
	}
	entry, _ := log[0].(map[string]any)
	if entry["summary"] != "shipped M1" || entry["run_id"] != "run-7" {
		t.Fatalf("log entry = %v", entry)
	}
	if entry["at"] == "" {
		t.Fatalf("log entry has no date: %v", entry)
	}
}

// The roster carries memory accounting so the UI can render a budget.
func TestRosterReportsMemoryUsage(t *testing.T) {
	ts, bot := newMemoryServer(t)
	postJSON(t, ts, "/api/v1/bots/"+bot.ID+"/memory", map[string]any{"text": "keeps notes"})

	roster := getJSON(t, ts, "/api/v1/roster")
	bots, _ := roster["bots"].([]any)
	var found map[string]any
	for _, b := range bots {
		m, _ := b.(map[string]any)
		if m["id"] == bot.ID {
			found = m
			break
		}
	}
	if found == nil {
		t.Fatalf("bot %s not in roster", bot.ID)
	}
	if used, _ := found["memory_used"].(float64); used == 0 {
		t.Fatalf("roster bot has no memory_used: %v", found)
	}
	if cap, _ := found["memory_cap"].(float64); cap == 0 {
		t.Fatalf("roster bot has no memory_cap: %v", found)
	}
}

func TestBotMemoryUnknownBotIs404(t *testing.T) {
	ts, _ := newMemoryServer(t)
	if st := getJSONStatus(t, ts, "/api/v1/bots/bot-nope/memory"); st.code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", st.code, st.body)
	}
}
