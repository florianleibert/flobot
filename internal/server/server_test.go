package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/store"
)

func TestCheckLoopbackAddr(t *testing.T) {
	if err := CheckLoopbackAddr("127.0.0.1:4120"); err != nil {
		t.Fatal(err)
	}
	if err := CheckLoopbackAddr("localhost:4120"); err != nil {
		t.Fatal(err)
	}
	if err := CheckLoopbackAddr("[::1]:4120"); err != nil {
		t.Fatal(err)
	}
	for _, addr := range []string{"0.0.0.0:4120", ":4120", "192.168.1.9:4120", "10.0.0.1:4120"} {
		if err := CheckLoopbackAddr(addr); err == nil {
			t.Fatalf("accepted %s", addr)
		}
	}
}

func TestCreateBotAndRunEvents(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New()
	srv := New(st, WithExec(&executor.Exec{Bin: bin}))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{
		"name": "Coder", "role": "coder", "route": "glm",
	})
	id, _ := bot["id"].(string)
	if !strings.HasPrefix(id, "bot-") {
		t.Fatalf("bot id %v", bot["id"])
	}
	run := postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": id, "prompt": "hello", "intent": "code",
	})
	runID, _ := run["id"].(string)
	if !strings.HasPrefix(runID, "run-") {
		t.Fatalf("run id %v", run["id"])
	}
	ev := waitEvents(t, ts, runID)
	kinds := eventKinds(ev)
	if !strings.Contains(kinds, "content") || !strings.Contains(kinds, "done") {
		t.Fatalf("events = %s raw=%v", kinds, ev)
	}
	raw, _ := json.Marshal(ev)
	if strings.Contains(string(raw), "secret-tool-arg") {
		t.Fatalf("tool args in events: %s", raw)
	}
}

func TestTestRunStartsAskIntent(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New()
	st.SetPersistPath(filepath.Join(dir, "state.json"))
	srv := New(st, WithExec(&executor.Exec{Bin: bin}), WithWorkDir(dir))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "C", "role": "coder", "route": "qwen"})
	sk := postJSON(t, ts, "/api/v1/skills", map[string]any{"name": "sysinfo", "body": "read uname only"})
	rt := postJSON(t, ts, "/api/v1/routines", map[string]any{
		"bot_id": bot["id"], "skill_id": sk["id"], "trigger": "manual",
	})
	out := postJSON(t, ts, "/api/v1/routines/"+rt["id"].(string)+"/test-run", map[string]any{})
	if out["last_run"] == nil || out["last_run"] == "" {
		t.Fatalf("LastRun unset: %v", out)
	}
	note, _ := out["last_note"].(string)
	if strings.Contains(note, "no external effect") {
		t.Fatalf("note: %s", note)
	}
	runID, _ := out["run_id"].(string)
	if runID == "" {
		t.Fatalf("missing run_id: %v", out)
	}
	waitEvents(t, ts, runID)
	en := postJSON(t, ts, "/api/v1/routines/"+rt["id"].(string)+"/enable", map[string]any{})
	if en["paused"] == true {
		t.Fatalf("still paused: %v", en)
	}

	// The roster must have been persisted to the state file.
	raw, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("state not persisted: %v", err)
	}
	if !strings.Contains(string(raw), bot["id"].(string)) {
		t.Fatalf("state file missing bot: %s", raw)
	}

	// A new server on the same store path must load the roster.
	st2 := store.New()
	st2.SetPersistPath(filepath.Join(dir, "state.json"))
	srv2 := New(st2, WithExec(&executor.Exec{Bin: bin}))
	if n := len(srv2.store.ListBots(Actor)); n != 1 {
		t.Fatalf("loaded bots = %d", n)
	}
}

func TestHealthModelsDoesNotIncludeSecrets(t *testing.T) {
	const secret = "TESTKEY_NOT_FOR_PROD_XYZ"
	t.Setenv("MODEL_API_KEY", secret)

	glm := modelsServer(t, []string{"glm-5.2-fp8"})
	qwen38 := modelsServer(t, []string{"gemma-4"})
	qwen36 := modelsServer(t, []string{"qwen3.6-27b-fp8"})
	muse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			http.Error(w, "no auth", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"muse-spark-1.2"}]}`)
	}))
	t.Cleanup(muse.Close)

	srv := New(store.New(), WithProbes([]Probe{
		{ID: "glm", URL: glm.URL + "/v1"},
		{ID: "qwen38", URL: qwen38.URL + "/v1"},
		{ID: "qwen36", URL: qwen36.URL + "/v1"},
		{ID: "muse", URL: muse.URL + "/v1", Auth: true},
	}))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/v1/health/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	low := strings.ToLower(string(body))
	for _, leak := range []string{secret, strings.ToLower(secret), "bearer ", "api_key", "authorization"} {
		if strings.Contains(low, strings.ToLower(leak)) && leak != "" {
			t.Fatalf("health JSON leaked %q: %s", leak, body)
		}
	}
	var wrap struct {
		Probes []ProbeResult `json:"probes"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		t.Fatal(err)
	}
	byID := map[string]ProbeResult{}
	for _, p := range wrap.Probes {
		byID[p.ID] = p
	}
	if !byID["glm"].OK || len(byID["glm"].IDs) == 0 {
		t.Fatalf("glm %+v", byID["glm"])
	}
	if !byID["qwen38"].QwenMismatch || byID["qwen38"].Error == "" {
		t.Fatalf("expected qwen_mismatch: %+v", byID["qwen38"])
	}
	if !strings.Contains(byID["qwen38"].Error, "qwen3.8") {
		t.Fatalf("mismatch error: %q", byID["qwen38"].Error)
	}
	if !byID["muse"].OK {
		t.Fatalf("muse %+v", byID["muse"])
	}
}

func TestIndexAndHealthAndLocalUnpinned(t *testing.T) {
	srv := New(store.New(), WithIndex("<html>flobot</html>"))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), "flobot") {
		t.Fatalf("index: %s", b)
	}
	resp, err = http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("health: %s", b)
	}
	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "L", "role": "tools", "route": "local"})
	resp, err = http.Post(ts.URL+"/api/v1/runs", "application/json", strings.NewReader(`{"bot_id":"`+bot["id"].(string)+`","prompt":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == 200 {
		t.Fatalf("local run should fail: %s", b)
	}
	if !strings.Contains(string(b), "unpinned") {
		t.Fatalf("local error: %s", b)
	}
}

func TestDefaultProbes(t *testing.T) {
	got := map[string]Probe{}
	for _, p := range DefaultProbes() {
		got[p.ID] = p
	}
	if got["glm"].URL != "http://10.30.0.127:13038/v1" {
		t.Fatalf("glm %s", got["glm"].URL)
	}
	if got["qwen38"].URL != "http://23.183.40.72:8020/v1" {
		t.Fatalf("qwen38 %s", got["qwen38"].URL)
	}
	if got["qwen36"].URL != "http://10.10.0.14:8000/v1" {
		t.Fatalf("qwen36 %s", got["qwen36"].URL)
	}
	if got["muse"].URL != "https://api.meta.ai/v1" || !got["muse"].Auth {
		t.Fatalf("muse %+v", got["muse"])
	}
}

func modelsServer(t *testing.T, ids []string) *httptest.Server {
	t.Helper()
	rows := make([]map[string]string, len(ids))
	for i, id := range ids {
		rows[i] = map[string]string{"id": id}
	}
	payload, _ := json.Marshal(map[string]any{"data": rows})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func postJSON(t *testing.T, ts *httptest.Server, path string, body map[string]any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(string(raw)))
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

func waitEvents(t *testing.T, ts *httptest.Server, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last map[string]any
	for time.Now().Before(deadline) {
		resp, err := http.Get(ts.URL + "/api/v1/runs/" + id + "/events")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		last = map[string]any{}
		if err := json.Unmarshal(b, &last); err != nil {
			t.Fatal(err)
		}
		st, _ := last["status"].(string)
		if st != "" && st != "running" {
			return last
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for run %s: %#v", id, last)
	return last
}

func eventKinds(ev map[string]any) string {
	arr, _ := ev["events"].([]any)
	var kinds []string
	for _, row := range arr {
		m, _ := row.(map[string]any)
		k, _ := m["kind"].(string)
		kinds = append(kinds, k)
	}
	return strings.Join(kinds, ",")
}
