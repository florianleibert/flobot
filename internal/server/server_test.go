package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/routes"
	"github.com/florianleibert/flobot/internal/schedule"
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
	loaded := srv2.store.ListBots(Actor)
	found := false
	for _, b := range loaded {
		if b.ID == bot["id"].(string) {
			found = true
		}
	}
	if !found {
		t.Fatalf("loaded bots missing created bot: %+v", loaded)
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

func TestStreamForwardsExecutorEvents(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(store.New(), WithExec(&executor.Exec{Bin: bin}), WithWorkDir(dir))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "C", "role": "coder", "route": "glm"})

	// Connect the stream client before starting the run.
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-ndjson" {
		t.Fatalf("content-type = %q", ct)
	}

	postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": bot["id"], "prompt": "hello", "intent": "code",
	})

	// Read until we see the done event (or a run_status), with a deadline.
	deadline := time.Now().Add(3 * time.Second)
	seen := map[string]bool{}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 256<<10)
	for time.Now().Before(deadline) {
		if !sc.Scan() {
			if sc.Err() != nil {
				t.Fatalf("stream read: %v", sc.Err())
			}
			break
		}
		var ev map[string]any
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("bad NDJSON line %q: %v", sc.Bytes(), err)
		}
		kind, _ := ev["kind"].(string)
		seen[kind] = true
		if kind == "done" || kind == "run_status" {
			break
		}
	}
	resp.Body.Close()
	if !seen["content"] {
		t.Fatalf("stream missed content event: %v", seen)
	}
	if !seen["done"] && !seen["run_status"] {
		t.Fatalf("stream missed terminal event: %v", seen)
	}
}

func TestStreamReconnectReplaysRunEvents(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(store.New(), WithExec(&executor.Exec{Bin: bin}), WithWorkDir(dir))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "C", "role": "coder", "route": "glm"})
	run := postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": bot["id"], "prompt": "hello", "intent": "code",
	})
	runID, _ := run["id"].(string)
	waitEvents(t, ts, runID)

	// First client connects, disconnects immediately, then reconnects with
	// ?run= and must receive the replayed events without the server crashing.
	resp, err := http.Get(ts.URL + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	resp, err = http.Get(ts.URL + "/api/stream?run=" + runID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	deadline := time.Now().Add(3 * time.Second)
	seen := map[string]bool{}
	sc := bufio.NewScanner(resp.Body)
	for time.Now().Before(deadline) && len(seen) < 2 {
		if !sc.Scan() {
			break
		}
		var ev map[string]any
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("bad NDJSON line %q: %v", sc.Bytes(), err)
		}
		kind, _ := ev["kind"].(string)
		seen[kind] = true
	}
	if !seen["content"] || !seen["done"] {
		t.Fatalf("reconnect replay missing events: %v", seen)
	}

	// The server must still be healthy after the disconnect/reconnect.
	health, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(health.Body)
	health.Body.Close()
	if health.StatusCode != 200 || !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("server unhealthy after reconnect: %d %s", health.StatusCode, b)
	}
}

func TestStreamDisconnectDoesNotCrashServer(t *testing.T) {
	srv := New(store.New(), WithIndex("<html>flobot</html>"))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// Abruptly drop several stream connections.
	for i := 0; i < 5; i++ {
		resp, err := http.Get(ts.URL + "/api/stream")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	// A fresh client must still be able to connect and the server must
	// answer health.
	resp, err := http.Get(ts.URL + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	health, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(health.Body)
	health.Body.Close()
	if health.StatusCode != 200 || !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("server unhealthy after disconnects: %d %s", health.StatusCode, b)
	}
}

func TestDefaultAddrIsLoopback(t *testing.T) {
	if DefaultAddr() != "127.0.0.1:4120" {
		t.Fatalf("DefaultAddr = %q", DefaultAddr())
	}
	if err := CheckLoopbackAddr(DefaultAddr()); err != nil {
		t.Fatal(err)
	}
}

// TestStopRunRecordsStoppedStatus proves a user-initiated stop finishes the
// run as "stopped", not the misleading "error" that a cancelled executor
// context would otherwise produce. Regression for the fix that added the
// stopped map to the server.
func TestStopRunRecordsStoppedStatus(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The fake binary sleeps 5s before emitting events, so there is a real
	// window in which the run is running and can be stopped.
	ex := &executor.Exec{
		Bin: bin,
		Environ: func() []string {
			return []string{"FLOCODE_FAKE_SLEEP=5"}
		},
	}
	srv := New(store.New(), WithExec(ex), WithWorkDir(dir))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "S", "role": "r", "route": "glm"})
	run := postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": bot["id"], "prompt": "hello", "intent": "code",
	})
	runID, _ := run["id"].(string)
	if runID == "" {
		t.Fatalf("run = %v", run)
	}

	// Stop the running run.
	resp, err := http.Post(ts.URL+"/api/v1/runs/"+runID+"/stop", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("stop -> %d %s", resp.StatusCode, b)
	}

	// The run must finish as "stopped", not "error".
	ev := waitEvents(t, ts, runID)
	if st, _ := ev["status"].(string); st != "stopped" {
		t.Fatalf("status = %q, want stopped (events=%s)", st, eventKinds(ev))
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

// fakeMeshCatalog serves an amesh-web /api/v2/mesh/models payload.
func fakeMeshCatalog(t *testing.T, models string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"schema":"amesh.mesh_model_catalog.v1","models":[%s]}`, models)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMeshModelsListsReadyRoutes(t *testing.T) {
	cat := fakeMeshCatalog(t,
		`{"id":"glm-5.2-fp8","display":"GLM 5.2","base_url":"https://10.30.0.8:18443/v1","health":"ready","where":"mesh","importable":true},
		 {"id":"qwen3.8-27b-fp8","health":"down","where":"mesh","importable":false}`)
	srv := New(store.New(), WithAmeshAddr(cat.Listener.Addr().String()))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res := getJSON(t, ts, "/api/v1/mesh/models")
	if res["ok"] != true {
		t.Fatalf("ok = %v (%v)", res["ok"], res["reason"])
	}
	rows, _ := res["models"].([]any)
	if len(rows) != 1 {
		t.Fatalf("models = %v", rows)
	}
	row, _ := rows[0].(map[string]any)
	if row["route"] != "mesh/glm-5.2-fp8" {
		t.Fatalf("route = %v", row["route"])
	}
}

func TestMeshModelsRendersUnreachableAmesh(t *testing.T) {
	srv := New(store.New(), WithAmeshAddr("127.0.0.1:1"))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	res := getJSON(t, ts, "/api/v1/mesh/models")
	if res["ok"] != false {
		t.Fatalf("ok = %v", res["ok"])
	}
	if res["reason"] == nil {
		t.Fatal("missing reason")
	}
}

func TestMeshRouteRunResolvesPinAndConfig(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	cat := fakeMeshCatalog(t,
		`{"id":"glm-5.2-fp8","base_url":"https://10.30.0.8:18443/v1","health":"ready","where":"mesh","importable":true}`)
	srv := New(store.New(), WithExec(&executor.Exec{Bin: bin}), WithWorkDir(dir), WithAmeshAddr(cat.Listener.Addr().String()))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{
		"name": "Meshy", "role": "coder", "route": "mesh/glm-5.2-fp8",
	})
	run := postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": bot["id"], "prompt": "hello", "intent": "ask",
	})
	runID, _ := run["id"].(string)
	if runID == "" {
		t.Fatalf("run = %v", run)
	}
	ev := waitEvents(t, ts, runID)
	kinds := eventKinds(ev)
	if !strings.Contains(kinds, "done") {
		t.Fatalf("events = %s", kinds)
	}
	if pin, _ := run["pin"].(string); pin != "glm-5.2-fp8" {
		t.Fatalf("pin = %q", pin)
	}
}

func TestMeshRouteRunRejectsUnknownModel(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	cat := fakeMeshCatalog(t,
		`{"id":"glm-5.2-fp8","base_url":"https://10.30.0.8:18443/v1","health":"ready","where":"mesh","importable":true}`)
	srv := New(store.New(), WithExec(&executor.Exec{Bin: bin}), WithAmeshAddr(cat.Listener.Addr().String()))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{
		"name": "Ghost", "role": "coder", "route": "mesh/nope",
	})
	res := postJSONStatus(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": bot["id"], "prompt": "hello",
	})
	if res.code != 400 {
		t.Fatalf("code = %d body = %v", res.code, res.body)
	}
}

func getJSON(t *testing.T, ts *httptest.Server, path string) map[string]any {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
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

func postJSONStatus(t *testing.T, ts *httptest.Server, path string, body map[string]any) struct {
	code int
	body string
} {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(string(raw)))
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

func TestHandoffRunSpawnsRecipientRun(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(store.New(), WithExec(&executor.Exec{Bin: bin}), WithWorkDir(dir))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	a := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "Chief", "role": "chief-of-staff", "route": "glm"})
	b := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "Coder", "role": "coder", "route": "qwen"})
	out := postJSON(t, ts, "/api/v1/handoff", map[string]any{
		"from": a["id"], "to": b["id"], "body": "fix the failing test", "run": true,
	})
	run, _ := out["run"].(map[string]any)
	if run == nil {
		t.Fatalf("no run in handoff response: %v", out)
	}
	runID, _ := run["id"].(string)
	if runID == "" {
		t.Fatalf("run id missing: %v", run)
	}
	if run["bot_id"] != b["id"] {
		t.Fatalf("run bot_id = %v, want recipient", run["bot_id"])
	}
	if run["intent"] != "code" {
		t.Fatalf("coder recipient should get intent code, got %v", run["intent"])
	}
	ev := waitEvents(t, ts, runID)
	if !strings.Contains(eventKinds(ev), "done") {
		t.Fatalf("events = %s", eventKinds(ev))
	}
}

func TestHandoffWithoutRunIsMessageOnly(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(store.New(), WithExec(&executor.Exec{Bin: bin}), WithWorkDir(dir))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	a := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "A", "role": "r", "route": "glm"})
	b := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "B", "role": "r", "route": "glm"})
	out := postJSON(t, ts, "/api/v1/handoff", map[string]any{
		"from": a["id"], "to": b["id"], "body": "note only",
	})
	if out["run"] != nil {
		t.Fatalf("unexpected run: %v", out["run"])
	}
	runs := getJSON(t, ts, "/api/v1/runs")
	if list, _ := runs["runs"].([]any); len(list) != 0 {
		t.Fatalf("handoff without run spawned runs: %v", list)
	}
}

// TestSchedulerFiresEnabledRoutine proves the server wiring end-to-end:
// fireables must carry Trigger/LastFire so Due() can match, and a due
// routine must spawn a run. Regression for the wiring bug where Fireable
// was built without Trigger, so the scheduler never fired anything.
func TestSchedulerFiresEnabledRoutine(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(store.New(), WithExec(&executor.Exec{Bin: bin}), WithWorkDir(dir))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "S", "role": "r", "route": "glm"})
	sk := postJSON(t, ts, "/api/v1/skills", map[string]any{"name": "s", "body": "how-to"})
	rt := postJSON(t, ts, "/api/v1/routines", map[string]any{
		"bot_id": bot["id"], "skill_id": sk["id"], "trigger": "every 1m",
	})
	// Test-run then enable: the store requires a test-run before enable.
	tr := postJSON(t, ts, "/api/v1/routines/"+rt["id"].(string)+"/test-run", map[string]any{})
	if tr["run_id"] == nil || tr["run_id"] == "" {
		t.Fatalf("test-run produced no run: %v", tr)
	}
	waitEvents(t, ts, tr["run_id"].(string))
	postJSON(t, ts, "/api/v1/routines/"+rt["id"].(string)+"/enable", map[string]any{})

	// The fireables list must include the enabled routine with its trigger.
	// (The store may also load routines persisted by earlier runs.)
	var mine []schedule.Fireable
	for _, f := range srv.fireables() {
		if f.RoutineID == rt["id"] {
			mine = append(mine, f)
		}
	}
	if len(mine) != 1 {
		t.Fatalf("fireables = %v", srv.fireables())
	}
	if mine[0].Trigger != "every 1m" {
		t.Fatalf("fireable trigger = %q", mine[0].Trigger)
	}
	fs := mine

	// Due() must fire after the interval elapses. Base the fake clock ahead
	// of the real wall clock so the test-run's LastFire is always in the past.
	sch := schedule.New()
	base := time.Now().Add(time.Hour)
	sch.Now = func() time.Time { return base }
	if fired := sch.Due(fs); len(fired) != 0 {
		t.Fatalf("fired on first observation: %v", fired)
	}
	sch.Now = func() time.Time { return base.Add(2 * time.Minute) }
	fired := sch.Due(fs)
	if len(fired) != 1 || fired[0].RoutineID != rt["id"] {
		t.Fatalf("fired = %v", fired)
	}

	// fireRoutine must spawn a run for the routine's bot.
	if err := srv.fireRoutine(fired[0]); err != nil {
		t.Fatalf("fireRoutine: %v", err)
	}
	runs := getJSON(t, ts, "/api/v1/runs")
	list, _ := runs["runs"].([]any)
	if len(list) < 2 {
		t.Fatalf("routine fire spawned no run: %v", list)
	}

	// The fire must be recorded on the routine so the card shows when it
	// last ran (the store keeps LastRun/LastNote, not the run itself).
	rtAfter, err := srv.store.GetRoutine(rt["id"].(string), Actor)
	if err != nil {
		t.Fatalf("get routine: %v", err)
	}
	if rtAfter.LastRun == "" {
		t.Fatalf("routine last_run not recorded: %+v", rtAfter)
	}
	if !strings.Contains(rtAfter.LastNote, "scheduled fire") {
		t.Fatalf("routine last_note = %q, want a scheduled-fire note", rtAfter.LastNote)
	}
}

// TestRememberAndRunWithMemory proves the remember endpoint stores a note
// and the note is prepended to the next run's prompt (teammate memory).
func TestRememberAndRunWithMemory(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Capture argv so the prompt the child received can be inspected.
	argvPath := filepath.Join(dir, "argv.txt")
	ex := &executor.Exec{
		Bin:     bin,
		Environ: func() []string { return []string{"FLOCODE_FAKE_ARGV=" + argvPath} },
	}
	srv := New(store.New(), WithExec(ex), WithWorkDir(dir))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "M", "role": "r", "route": "glm"})
	if bot["memory"] != nil && bot["memory"] != "" {
		t.Fatalf("fresh bot has memory: %v", bot["memory"])
	}
	got := postJSON(t, ts, "/api/v1/bots/"+bot["id"].(string)+"/remember", map[string]any{
		"note": "always write tests first",
	})
	if got["memory"] != "always write tests first" {
		t.Fatalf("memory = %v", got["memory"])
	}
	// Empty note is rejected.
	if st := postJSONStatus(t, ts, "/api/v1/bots/"+bot["id"].(string)+"/remember", map[string]any{"note": " "}); st.code != 400 {
		t.Fatalf("empty note status = %d", st.code)
	}

	run := postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": bot["id"], "prompt": "do the thing", "intent": "ask",
	})
	waitEvents(t, ts, run["id"].(string))

	b, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	argv := string(b)
	if !strings.Contains(argv, "always write tests first") {
		t.Fatalf("memory not prepended to child prompt: %s", argv)
	}
	if !strings.Contains(argv, "do the thing") {
		t.Fatalf("prompt missing: %s", argv)
	}
}

func TestLocalPinEndpointAndHealthReflectsIt(t *testing.T) {
	srv := New(store.New())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	// New() loads persisted state, which may restore a local pin written by
	// an earlier test; force unpinned so this test controls the lifecycle.
	routes.SetLocalPin("")
	defer routes.SetLocalPin("")

	// Unpinned status.
	res := getJSON(t, ts, "/api/v1/local")
	if res["pinned"] != false {
		t.Fatalf("unpinned local: %v", res)
	}

	// Health reports unpinned.
	h := getJSON(t, ts, "/api/v1/health/models")
	probes, _ := h["probes"].([]any)
	var local ProbeResult
	for _, p := range probes {
		m, _ := p.(map[string]any)
		if m["id"] == "local" {
			b, _ := json.Marshal(m)
			_ = json.Unmarshal(b, &local)
		}
	}
	if local.OK || !strings.Contains(local.Error, "unpinned") {
		t.Fatalf("unpinned local health: %+v", local)
	}

	// Pin local via the endpoint.
	resp, err := http.Post(ts.URL+"/api/v1/local/pin", "application/json",
		strings.NewReader(`{"pin":"qwen3-coder-30b-a3b-mlx"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("pin local -> %d %s", resp.StatusCode, b)
	}
	var pinned map[string]any
	if err := json.Unmarshal(b, &pinned); err != nil {
		t.Fatal(err)
	}
	if pinned["pinned"] != true || pinned["pin"] != "qwen3-coder-30b-a3b-mlx" {
		t.Fatalf("pinned: %v", pinned)
	}

	// Health now reflects the pin.
	h = getJSON(t, ts, "/api/v1/health/models")
	probes, _ = h["probes"].([]any)
	for _, p := range probes {
		m, _ := p.(map[string]any)
		if m["id"] == "local" {
			b, _ := json.Marshal(m)
			_ = json.Unmarshal(b, &local)
		}
	}
	if !strings.Contains(local.Error, "pinned to qwen3-coder-30b-a3b-mlx") {
		t.Fatalf("pinned local health: %+v", local)
	}
}

func TestBotCreateAcceptsLocalAndRunFailsUntilPinned(t *testing.T) {
	srv := New(store.New())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	// New() loads persisted state, which may restore a local pin written by
	// an earlier test; force unpinned so this test controls the lifecycle.
	routes.SetLocalPin("")
	defer routes.SetLocalPin("")

	// A bot on the local route can be created even before the route is
	// pinned (shape-valid); the failure surfaces at run time.
	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "L", "role": "tools", "route": "local"})
	bid, _ := bot["id"].(string)

	// Running on an unpinned local route fails with a clear error.
	resp, err := http.Post(ts.URL+"/api/v1/runs", "application/json",
		strings.NewReader(`{"bot_id":"`+bid+`","prompt":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == 200 {
		t.Fatalf("unpinned local run should fail: %s", b)
	}
	if !strings.Contains(string(b), "unpinned") {
		t.Fatalf("unpinned local run error: %s", b)
	}

	// Pin local; now the same bot can run.
	routes.SetLocalPin("qwen3-coder-30b-a3b-mlx")
	defer routes.SetLocalPin("")
	resp, err = http.Post(ts.URL+"/api/v1/runs", "application/json",
		strings.NewReader(`{"bot_id":"`+bid+`","prompt":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("pinned local run -> %d %s", resp.StatusCode, b)
	}
}
