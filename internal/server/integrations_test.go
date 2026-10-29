package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/store"
)

// TestIntegrationCreateListFireFlow exercises the full lifecycle: create an
// integration, bind a routine to it, fire it with the secret, and confirm a
// run starts and the fire is recorded.
func TestIntegrationCreateListFireFlow(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New()
	st.SetPersistPath(filepath.Join(dir, "state.json"))
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(dir, ".flobot", "integrations.json"))
	srv := New(st, WithExec(&executor.Exec{Bin: bin}), WithWorkDir(dir), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// Create a bot + skill + routine to bind.
	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "C", "role": "coder", "route": "glm"})
	sk := postJSON(t, ts, "/api/v1/skills", map[string]any{"name": "s", "body": "how-to"})
	rt := postJSON(t, ts, "/api/v1/routines", map[string]any{
		"bot_id": bot["id"], "skill_id": sk["id"], "trigger": "manual",
	})

	// Create an integration; the secret is returned exactly once.
	res := postJSON(t, ts, "/api/v1/integrations", map[string]any{
		"kind": "gmail", "name": "work inbox", "detail": "label: inbox",
	})
	in, _ := res["integration"].(map[string]any)
	intID, _ := in["id"].(string)
	secret, _ := in["secret"].(string)
	if intID == "" || secret == "" {
		t.Fatalf("integration = %v", res)
	}

	// The list endpoint never returns the secret.
	list := getJSON(t, ts, "/api/v1/integrations")
	rows, _ := list["integrations"].([]any)
	if len(rows) != 1 {
		t.Fatalf("integrations = %v", rows)
	}
	row, _ := rows[0].(map[string]any)
	if _, has := row["secret"]; has {
		t.Fatal("list endpoint leaked the secret")
	}

	// Bind the routine to the integration.
	bind := postJSON(t, ts, "/api/v1/routines/"+rt["id"].(string)+"/integration", map[string]any{
		"integration_id": intID,
	})
	if bind["integration_id"] != intID {
		t.Fatalf("bind = %v", bind)
	}

	// A routine must be test-run and enabled before it can fire (same gate
	// as the scheduler).
	out := postJSON(t, ts, "/api/v1/routines/"+rt["id"].(string)+"/test-run", map[string]any{})
	runID, _ := out["run_id"].(string)
	if runID == "" {
		t.Fatalf("test-run = %v", out)
	}
	waitEvents(t, ts, runID)
	en := postJSON(t, ts, "/api/v1/routines/"+rt["id"].(string)+"/enable", map[string]any{})
	if en["paused"] == true {
		t.Fatalf("routine still paused after enable: %v", en)
	}

	// Fire with the wrong secret is refused.
	fireBad := postJSONStatus(t, ts, "/api/v1/integrations/"+intID+"/fire", map[string]any{"text": "hi"})
	if fireBad.code != 401 {
		t.Fatalf("bad-secret fire -> %d %s", fireBad.code, fireBad.body)
	}

	// Fire with the right secret starts a run.
	fireReq, _ := json.Marshal(map[string]any{"text": "new mail about X"})
	fireHTTP, err := http.NewRequest("POST", ts.URL+"/api/v1/integrations/"+intID+"/fire",
		strings.NewReader(string(fireReq)))
	if err != nil {
		t.Fatal(err)
	}
	fireHTTP.Header.Set("Content-Type", "application/json")
	fireHTTP.Header.Set("Authorization", "Bearer "+secret)
	fireResp, err := http.DefaultClient.Do(fireHTTP)
	if err != nil {
		t.Fatal(err)
	}
	fireBody, _ := io.ReadAll(fireResp.Body)
	fireResp.Body.Close()
	if fireResp.StatusCode >= 300 {
		t.Fatalf("fire -> %d %s", fireResp.StatusCode, fireBody)
	}
	var fired map[string]any
	if err := json.Unmarshal(fireBody, &fired); err != nil {
		t.Fatal(err)
	}
	if fired["fired"] != float64(1) {
		t.Fatalf("fired = %v", fired)
	}

	// A run was created and finished.
	runs := getJSON(t, ts, "/api/v1/runs")
	arr, _ := runs["runs"].([]any)
	if len(arr) == 0 {
		t.Fatal("no run after fire")
	}
	run0, _ := arr[0].(map[string]any)
	firedRunID, _ := run0["id"].(string)
	if firedRunID == "" {
		t.Fatalf("run = %v", run0)
	}
	done := waitEvents(t, ts, firedRunID)
	if done["status"] != "done" {
		t.Fatalf("run status = %v", done["status"])
	}

	// The fire event is recorded on the integration.
	list2 := getJSON(t, ts, "/api/v1/integrations")
	rows2, _ := list2["integrations"].([]any)
	row2, _ := rows2[0].(map[string]any)
	evs, _ := row2["events"].([]any)
	if len(evs) == 0 {
		t.Fatal("no fire event recorded")
	}
}

// TestIntegrationFireRequiresSecret proves the webhook receiver refuses an
// unauthenticated request even when the integration exists.
func TestIntegrationFireRequiresSecret(t *testing.T) {
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(t.TempDir(), ".flobot", "integrations.json"))
	srv := New(store.New(), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res := postJSON(t, ts, "/api/v1/integrations", map[string]any{
		"kind": "webhook", "name": "hook",
	})
	in, _ := res["integration"].(map[string]any)
	intID, _ := in["id"].(string)

	resp, err := http.Post(ts.URL+"/api/v1/integrations/"+intID+"/fire", "application/json",
		strings.NewReader(`{"text":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Fatalf("unauthenticated fire -> %d %s", resp.StatusCode, b)
	}
}

// TestIntegrationDeleteUnbindsRoutines proves that deleting an integration
// unbinds the routines that fired on it.
func TestIntegrationDeleteUnbindsRoutines(t *testing.T) {
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(t.TempDir(), ".flobot", "integrations.json"))
	srv := New(store.New(), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "C", "role": "coder", "route": "glm"})
	sk := postJSON(t, ts, "/api/v1/skills", map[string]any{"name": "s", "body": "how-to"})
	rt := postJSON(t, ts, "/api/v1/routines", map[string]any{
		"bot_id": bot["id"], "skill_id": sk["id"], "trigger": "manual",
	})
	res := postJSON(t, ts, "/api/v1/integrations", map[string]any{
		"kind": "webhook", "name": "hook",
	})
	in, _ := res["integration"].(map[string]any)
	intID, _ := in["id"].(string)

	bind := postJSON(t, ts, "/api/v1/routines/"+rt["id"].(string)+"/integration", map[string]any{
		"integration_id": intID,
	})
	if bind["integration_id"] != intID {
		t.Fatalf("bind = %v", bind)
	}

	resp, err := http.NewRequest("DELETE", ts.URL+"/api/v1/integrations/"+intID, nil)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := http.DefaultClient.Do(resp)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != 204 {
		t.Fatalf("delete -> %d", r2.StatusCode)
	}

	rts := getJSON(t, ts, "/api/v1/roster")
	routArr, _ := rts["routines"].([]any)
	for _, rr := range routArr {
		m, _ := rr.(map[string]any)
		if m["id"] == rt["id"] {
			if v, has := m["integration_id"]; has && v != "" {
				t.Fatalf("routine still bound after integration delete: %v", m)
			}
		}
	}
}

// TestIntegrationBindUnknownRejected proves binding to a nonexistent
// integration is refused.
func TestIntegrationBindUnknownRejected(t *testing.T) {
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(t.TempDir(), ".flobot", "integrations.json"))
	srv := New(store.New(), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "C", "role": "coder", "route": "glm"})
	sk := postJSON(t, ts, "/api/v1/skills", map[string]any{"name": "s", "body": "how-to"})
	rt := postJSON(t, ts, "/api/v1/routines", map[string]any{
		"bot_id": bot["id"], "skill_id": sk["id"], "trigger": "manual",
	})
	res := postJSONStatus(t, ts, "/api/v1/routines/"+rt["id"].(string)+"/integration", map[string]any{
		"integration_id": "int-nope",
	})
	if res.code != 400 {
		t.Fatalf("bind unknown -> %d %s", res.code, res.body)
	}
}

// armRoutine creates a bot, a skill and a routine, test-runs it and enables
// it, so the routine is allowed to fire. It returns the routine id.
func armRoutine(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "C", "role": "coder", "route": "glm"})
	sk := postJSON(t, ts, "/api/v1/skills", map[string]any{"name": "s", "body": "how-to"})
	rt := postJSON(t, ts, "/api/v1/routines", map[string]any{
		"bot_id": bot["id"], "skill_id": sk["id"], "trigger": "manual",
	})
	rtID, _ := rt["id"].(string)
	out := postJSON(t, ts, "/api/v1/routines/"+rtID+"/test-run", map[string]any{})
	runID, _ := out["run_id"].(string)
	if runID == "" {
		t.Fatalf("test-run = %v", out)
	}
	waitEvents(t, ts, runID)
	postJSON(t, ts, "/api/v1/routines/"+rtID+"/enable", map[string]any{})
	return rtID
}

// createIntegration creates an integration and returns its id and secret.
func createIntegration(t *testing.T, ts *httptest.Server, kind, name, authMode string) (string, string) {
	t.Helper()
	res := postJSON(t, ts, "/api/v1/integrations", map[string]any{
		"kind": kind, "name": name, "auth_mode": authMode,
	})
	in, _ := res["integration"].(map[string]any)
	if in == nil {
		t.Fatalf("create integration = %v", res)
	}
	id, _ := in["id"].(string)
	secret, _ := in["secret"].(string)
	if id == "" || secret == "" {
		t.Fatalf("integration = %v", in)
	}
	return id, secret
}

// fireWith posts a webhook body with custom headers and returns the status
// code and body.
func fireWith(t *testing.T, ts *httptest.Server, path, body string, headers map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("POST", ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// TestIntegrationProvidersEndpoint proves the connect catalogue is served:
// every provider kind with its default scheme, the schemes it accepts and
// the connect steps.
func TestIntegrationProvidersEndpoint(t *testing.T) {
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(t.TempDir(), ".flobot", "integrations.json"))
	srv := New(store.New(), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res := getJSON(t, ts, "/api/v1/integrations/providers")
	provs, _ := res["providers"].([]any)
	if len(provs) < 8 {
		t.Fatalf("providers = %d, want at least 8", len(provs))
	}
	kinds := map[string]map[string]any{}
	for _, p := range provs {
		m, _ := p.(map[string]any)
		kind, _ := m["kind"].(string)
		if kind == "" {
			t.Fatalf("provider without kind: %v", m)
		}
		if m["label"] == "" || m["blurb"] == "" || m["default_auth"] == "" {
			t.Fatalf("provider %q is missing copy: %v", kind, m)
		}
		if steps, _ := m["steps"].([]any); len(steps) < 3 {
			t.Fatalf("provider %q has %d steps", kind, len(steps))
		}
		kinds[kind] = m
	}
	for _, want := range []string{"gmail", "slack", "github", "telegram", "discord", "linear", "stripe", "webhook"} {
		if _, ok := kinds[want]; !ok {
			t.Fatalf("provider %q missing from catalogue", want)
		}
	}
	modes, _ := res["auth_modes"].([]any)
	if len(modes) != len(store.AuthModes) {
		t.Fatalf("auth modes = %d, want %d", len(modes), len(store.AuthModes))
	}
}

// TestIntegrationViewShape proves the list view carries everything the UI
// needs — URL, secret hint, provider copy, status — and never the secret.
func TestIntegrationViewShape(t *testing.T) {
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(t.TempDir(), ".flobot", "integrations.json"))
	srv := New(store.New(), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	id, secret := createIntegration(t, ts, "github", "repo hooks", "hmac")
	res := getJSON(t, ts, "/api/v1/integrations")
	rows, _ := res["integrations"].([]any)
	if len(rows) != 1 {
		t.Fatalf("integrations = %v", rows)
	}
	row, _ := rows[0].(map[string]any)
	if _, has := row["secret"]; has {
		t.Fatal("list view leaked the secret")
	}
	if row["auth_mode"] != "hmac" {
		t.Fatalf("auth_mode = %v", row["auth_mode"])
	}
	if !strings.Contains(fmt.Sprint(row["url"]), id+"/fire") {
		t.Fatalf("url = %v", row["url"])
	}
	if row["secret_hint"] == "" {
		t.Fatal("secret hint missing")
	}
	if strings.Contains(fmt.Sprint(row["secret_hint"]), secret) {
		t.Fatal("secret hint leaks the secret")
	}
	if row["status"] != "unbound" {
		t.Fatalf("status = %v, want unbound", row["status"])
	}
	prov, _ := row["provider"].(map[string]any)
	if prov["label"] != "GitHub" {
		t.Fatalf("provider = %v", prov)
	}
}

// waitRuns waits for every run started by a fire response to finish, so the
// next fire is not blocked by the bot still working.
func waitRuns(t *testing.T, ts *httptest.Server, body string) {
	t.Helper()
	var res struct {
		Runs []string `json:"runs"`
	}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("fire response %q: %v", body, err)
	}
	for _, id := range res.Runs {
		waitEvents(t, ts, id)
	}
}

// TestIntegrationAuthModes proves the receiver honours the scheme the
// integration was created with: a URL key, a GitHub-style HMAC header, and a
// bearer header — and refuses every other credential.
func TestIntegrationAuthModes(t *testing.T) {
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(t.TempDir(), ".flobot", "integrations.json"))
	srv := New(store.New(), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	rtID := armRoutine(t, ts)

	// Query key.
	qID, qSecret := createIntegration(t, ts, "webhook", "query hook", "query")
	postJSON(t, ts, "/api/v1/routines/"+rtID+"/integration", map[string]any{"integration_id": qID})
	code, body := fireWith(t, ts, "/api/v1/integrations/"+qID+"/fire?key="+qSecret, `{"text":"hi"}`, nil)
	if code != 200 {
		t.Fatalf("query fire -> %d %s", code, body)
	}
	waitRuns(t, ts, body)
	code, _ = fireWith(t, ts, "/api/v1/integrations/"+qID+"/fire?key=wrong", `{"text":"hi"}`, nil)
	if code != 401 {
		t.Fatalf("query fire with a wrong key -> %d", code)
	}

	// GitHub-style HMAC.
	hID, hSecret := createIntegration(t, ts, "github", "hmac hook", "hmac")
	postJSON(t, ts, "/api/v1/routines/"+rtID+"/integration", map[string]any{"integration_id": hID})
	payload := `{"action":"opened","repository":{"full_name":"flobot/demo"},"issue":{"number":7,"title":"Fix the thing"}}`
	mac := hmac.New(sha256.New, []byte(hSecret))
	mac.Write([]byte(payload))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	code, body = fireWith(t, ts, "/api/v1/integrations/"+hID+"/fire", payload, map[string]string{
		"X-Hub-Signature-256": sig,
	})
	if code != 200 {
		t.Fatalf("hmac fire -> %d %s", code, body)
	}
	waitRuns(t, ts, body)
	code, _ = fireWith(t, ts, "/api/v1/integrations/"+hID+"/fire", payload, map[string]string{
		"X-Hub-Signature-256": "sha256=deadbeef",
	})
	if code != 401 {
		t.Fatalf("hmac fire with a bad signature -> %d", code)
	}
}

// TestIntegrationSlackSignature proves the Slack scheme verifies the v0
// signature over "v0:<timestamp>:<body>" and refuses a stale timestamp.
func TestIntegrationSlackSignature(t *testing.T) {
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(t.TempDir(), ".flobot", "integrations.json"))
	srv := New(store.New(), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	rtID := armRoutine(t, ts)
	id, secret := createIntegration(t, ts, "slack", "slack hook", "slack")
	postJSON(t, ts, "/api/v1/routines/"+rtID+"/integration", map[string]any{"integration_id": id})

	body := `{"text":"hello from slack"}`
	ts2 := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + ts2 + ":" + body))
	code, respBody := fireWith(t, ts, "/api/v1/integrations/"+id+"/fire", body, map[string]string{
		"X-Slack-Request-Timestamp": ts2,
		"X-Slack-Signature":         "v0=" + hex.EncodeToString(mac.Sum(nil)),
	})
	if code != 200 {
		t.Fatalf("slack fire -> %d %s", code, respBody)
	}

	// A stale timestamp is refused even with a valid signature.
	old := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	mac2 := hmac.New(sha256.New, []byte(secret))
	mac2.Write([]byte("v0:" + old + ":" + body))
	code, _ = fireWith(t, ts, "/api/v1/integrations/"+id+"/fire", body, map[string]string{
		"X-Slack-Request-Timestamp": old,
		"X-Slack-Signature":         "v0=" + hex.EncodeToString(mac2.Sum(nil)),
	})
	if code != 401 {
		t.Fatalf("stale slack fire -> %d", code)
	}
}

// TestIntegrationRotateSecret proves rotation hands out a new secret exactly
// once and kills the old one.
func TestIntegrationRotateSecret(t *testing.T) {
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(t.TempDir(), ".flobot", "integrations.json"))
	srv := New(store.New(), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	rtID := armRoutine(t, ts)
	id, oldSecret := createIntegration(t, ts, "webhook", "rotate me", "bearer")
	postJSON(t, ts, "/api/v1/routines/"+rtID+"/integration", map[string]any{"integration_id": id})

	res := postJSON(t, ts, "/api/v1/integrations/"+id+"/rotate", map[string]any{})
	in, _ := res["integration"].(map[string]any)
	newSecret, _ := in["secret"].(string)
	if newSecret == "" || newSecret == oldSecret {
		t.Fatalf("rotate = %v", res)
	}
	code, _ := fireWith(t, ts, "/api/v1/integrations/"+id+"/fire", `{"text":"x"}`, map[string]string{
		"Authorization": "Bearer " + oldSecret,
	})
	if code != 401 {
		t.Fatalf("old secret still works -> %d", code)
	}
	code, body := fireWith(t, ts, "/api/v1/integrations/"+id+"/fire", `{"text":"x"}`, map[string]string{
		"Authorization": "Bearer " + newSecret,
	})
	if code != 200 {
		t.Fatalf("new secret fire -> %d %s", code, body)
	}
}

// TestIntegrationTestEndpoint proves the "send a test event" path fires the
// bound routine with a realistic provider payload.
func TestIntegrationTestEndpoint(t *testing.T) {
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(t.TempDir(), ".flobot", "integrations.json"))
	srv := New(store.New(), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	rtID := armRoutine(t, ts)
	id, _ := createIntegration(t, ts, "github", "test hook", "hmac")
	postJSON(t, ts, "/api/v1/routines/"+rtID+"/integration", map[string]any{"integration_id": id})

	res := postJSON(t, ts, "/api/v1/integrations/"+id+"/test", map[string]any{})
	if res["fired"] != float64(1) {
		t.Fatalf("test fire = %v", res)
	}
	preview, _ := res["preview"].(string)
	if !strings.Contains(preview, "GitHub:") {
		t.Fatalf("preview = %q", preview)
	}
	runs, _ := res["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("runs = %v", runs)
	}
}

// TestIntegrationFireWithoutRoutine proves an unbound integration refuses to
// fire with a clear error instead of pretending it worked.
func TestIntegrationFireWithoutRoutine(t *testing.T) {
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(t.TempDir(), ".flobot", "integrations.json"))
	srv := New(store.New(), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	id, secret := createIntegration(t, ts, "webhook", "lonely", "bearer")
	code, body := fireWith(t, ts, "/api/v1/integrations/"+id+"/fire", `{"text":"x"}`, map[string]string{
		"Authorization": "Bearer " + secret,
	})
	if code != 202 || !strings.Contains(body, "no routines bound") {
		t.Fatalf("unbound fire -> %d %s", code, body)
	}
	// The event is still recorded: the operator can see it in the log.
	evs := getJSON(t, ts, "/api/v1/integrations/"+id+"/events")
	list, _ := evs["events"].([]any)
	if len(list) != 1 {
		t.Fatalf("events = %v", list)
	}
	ev, _ := list[0].(map[string]any)
	if ev["status"] != "ignored" {
		t.Fatalf("event status = %v", ev["status"])
	}
}

// TestEventTextNormalization proves every provider payload becomes a
// readable one-liner for the agent.
func TestEventTextNormalization(t *testing.T) {
	cases := []struct {
		kind string
		body string
		want string
	}{
		{store.IntegrationGmail, `{"from":"a@b.c","subject":"Hi","text":"Body"}`, "Gmail: Body"},
		{store.IntegrationSlack, `{"text":"hi team"}`, "Slack: hi team"},
		{store.IntegrationTelegram, `{"message":{"text":"ping"}}`, "Telegram: ping"},
		{store.IntegrationDiscord, `{"content":"yo"}`, "Discord: yo"},
		{store.IntegrationGitHub, `{"action":"opened","repository":{"full_name":"o/r"},"issue":{"number":3,"title":"Bug"},"sender":{"login":"me"}}`, "GitHub: opened o/r #3 Bug (by me)"},
		{store.IntegrationLinear, `{"type":"Issue","data":{"title":"Fix it"}}`, "Linear: Issue Fix it"},
		{store.IntegrationStripe, `{"type":"charge.succeeded","data":{"object":{"description":"Pro plan"}}}`, "Stripe: charge.succeeded Pro plan"},
		{store.IntegrationWebhook, `{"text":"plain"}`, "plain"},
	}
	for _, c := range cases {
		got := eventText(c.kind, []byte(c.body))
		if got != c.want {
			t.Errorf("eventText(%s) = %q, want %q", c.kind, got, c.want)
		}
	}
}

// TestIntegrationEventsAreAttributed proves a rejected event lands on the
// integration it came from — and on no other.
func TestIntegrationEventsAreAttributed(t *testing.T) {
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(t.TempDir(), ".flobot", "integrations.json"))
	srv := New(store.New(), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	aID, aSecret := createIntegration(t, ts, "webhook", "a", "bearer")
	bID, _ := createIntegration(t, ts, "webhook", "b", "bearer")

	// A wrong credential is refused and recorded.
	code, _ := fireWith(t, ts, "/api/v1/integrations/"+aID+"/fire", `{"text":"x"}`, map[string]string{
		"Authorization": "Bearer wrong",
	})
	if code != 401 {
		t.Fatalf("bad credential -> %d", code)
	}
	evA := getJSON(t, ts, "/api/v1/integrations/"+aID+"/events")
	rowsA, _ := evA["events"].([]any)
	if len(rowsA) != 1 {
		t.Fatalf("events on a = %v", rowsA)
	}
	ev, _ := rowsA[0].(map[string]any)
	if ev["status"] != "rejected" {
		t.Fatalf("status = %v", ev["status"])
	}
	if ev["integration_id"] != aID {
		t.Fatalf("integration id = %v", ev["integration_id"])
	}

	// The other integration has none of them.
	evB := getJSON(t, ts, "/api/v1/integrations/"+bID+"/events")
	rowsB, _ := evB["events"].([]any)
	if len(rowsB) != 0 {
		t.Fatalf("events on b = %v", rowsB)
	}
	_ = aSecret
}
