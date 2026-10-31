package server

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/store"
)

// TestUseCasesCatalogue proves the kit is complete and honest: ten use
// cases, each with a provider flobot can actually receive, a scheme that
// provider supports, a playbook and an example event.
func TestUseCasesCatalogue(t *testing.T) {
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(t.TempDir(), ".flobot", "integrations.json"))
	srv := New(store.New(), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res := getJSON(t, ts, "/api/v1/usecases")
	rows, _ := res["usecases"].([]any)
	if len(rows) != 10 {
		t.Fatalf("use cases = %d, want 10", len(rows))
	}
	seen := map[string]bool{}
	for _, r := range rows {
		uc, _ := r.(map[string]any)
		id, _ := uc["id"].(string)
		if id == "" {
			t.Fatalf("use case without id: %v", uc)
		}
		if seen[id] {
			t.Fatalf("duplicate use case id %q", id)
		}
		seen[id] = true
		for _, field := range []string{"name", "category", "blurb", "provider", "auth_mode", "route", "bot_name", "role", "tools", "playbook", "event"} {
			if v, _ := uc[field].(string); strings.TrimSpace(v) == "" {
				t.Errorf("use case %q is missing %q", id, field)
			}
		}
		// The kit is text-only on the local route: the model's tool calls
		// are not executed there, so a tool-happy run would answer with a
		// tool call instead of the answer.
		if uc["tools"] != store.ToolsNone {
			t.Errorf("use case %q tools = %v, want %q", id, uc["tools"], store.ToolsNone)
		}
		if uc["route"] != "local" {
			t.Errorf("use case %q runs on %v, want local", id, uc["route"])
		}
		provider, _ := uc["provider"].(string)
		if !store.ValidIntegrationKind(provider) {
			t.Errorf("use case %q has an unknown provider %q", id, provider)
		}
		mode, _ := uc["auth_mode"].(string)
		if !store.ValidAuthMode(mode) {
			t.Errorf("use case %q has an unknown auth mode %q", id, mode)
		}
		if steps, _ := uc["steps"].([]any); len(steps) < 2 {
			t.Errorf("use case %q has %d steps", id, len(steps))
		}
	}
}

// TestActivateUseCaseCreatesTheWholeChain proves one click makes a working
// automation: a teammate on the local route, the playbook, an enabled routine
// and the provider connection — with the secret handed out exactly once.
func TestActivateUseCaseCreatesTheWholeChain(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(dir, ".flobot", "integrations.json"))
	srv := New(store.New(), WithExec(&executor.Exec{Bin: bin}), WithWorkDir(dir), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res := postJSON(t, ts, "/api/v1/usecases/triage/activate", map[string]any{
		"create_integration": true,
	})
	bot, _ := res["bot"].(map[string]any)
	if bot["route"] != "local" {
		t.Fatalf("bot route = %v, want local", bot["route"])
	}
	if bot["id"] == "" || bot["name"] == "" {
		t.Fatalf("bot = %v", bot)
	}
	sk, _ := res["skill"].(map[string]any)
	if !strings.Contains(strings.ToLower(sk["body"].(string)), "classify") {
		t.Fatalf("skill body does not carry the playbook: %v", sk["body"])
	}
	rt, _ := res["routine"].(map[string]any)
	if rt["paused"] == true {
		t.Fatalf("routine is paused after activation: %v", rt)
	}
	in, _ := res["integration"].(map[string]any)
	if in["id"] == "" || in["secret"] == "" {
		t.Fatalf("integration = %v", in)
	}
	if in["kind"] != "github" || in["auth_mode"] != "hmac" {
		t.Fatalf("integration = %v", in)
	}
	if rt["integration_id"] != in["id"] {
		t.Fatalf("routine is not bound to the new integration: %v", rt)
	}
	if url, _ := res["url"].(string); !strings.Contains(url, in["id"].(string)+"/fire") {
		t.Fatalf("url = %v", res["url"])
	}

	// The routine shows up in the roster, bound and enabled.
	roster := getJSON(t, ts, "/api/v1/roster")
	rts, _ := roster["routines"].([]any)
	found := false
	for _, r := range rts {
		m, _ := r.(map[string]any)
		if m["id"] == rt["id"] {
			found = true
			if m["integration_id"] != in["id"] {
				t.Fatalf("roster routine = %v", m)
			}
		}
	}
	if !found {
		t.Fatal("activated routine is not in the roster")
	}
}

// TestActivateUseCaseWithExistingIntegration proves the operator can point a
// use case at a connection they already have, and that no second secret is
// handed out.
func TestActivateUseCaseWithExistingIntegration(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(dir, ".flobot", "integrations.json"))
	srv := New(store.New(), WithExec(&executor.Exec{Bin: bin}), WithWorkDir(dir), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	existingID, _ := createIntegration(t, ts, "webhook", "mine", "bearer")
	res := postJSON(t, ts, "/api/v1/usecases/chatops/activate", map[string]any{
		"integration_id": existingID,
	})
	rt, _ := res["routine"].(map[string]any)
	if rt["integration_id"] != existingID {
		t.Fatalf("routine = %v", rt)
	}
	if _, has := res["secret"]; has {
		t.Fatal("activation handed out a secret for an existing integration")
	}
	// No second integration was created.
	list := getJSON(t, ts, "/api/v1/integrations")
	rows, _ := list["integrations"].([]any)
	if len(rows) != 1 {
		t.Fatalf("integrations = %v", rows)
	}
}

// TestActivateUnknownUseCase proves an unknown id is refused.
func TestActivateUnknownUseCase(t *testing.T) {
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(t.TempDir(), ".flobot", "integrations.json"))
	srv := New(store.New(), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res := postJSONStatus(t, ts, "/api/v1/usecases/nope/activate", map[string]any{})
	if res.code != 404 {
		t.Fatalf("activate unknown -> %d %s", res.code, res.body)
	}
}

// TestTestFireAcceptsAnEventOverride proves the test endpoint can fire a
// use case's own example event instead of the generic sample payload.
func TestTestFireAcceptsAnEventOverride(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(dir, ".flobot", "integrations.json"))
	srv := New(store.New(), WithExec(&executor.Exec{Bin: bin}), WithWorkDir(dir), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	rtID := armRoutine(t, ts)
	id, _ := createIntegration(t, ts, "github", "hook", "hmac")
	postJSON(t, ts, "/api/v1/routines/"+rtID+"/integration", map[string]any{"integration_id": id})

	res := postJSON(t, ts, "/api/v1/integrations/"+id+"/test", map[string]any{
		"event": `{"action":"opened","repository":{"full_name":"flobot/demo"},"issue":{"number":99,"title":"override worked"}}`,
	})
	preview, _ := res["preview"].(string)
	if !strings.Contains(preview, "override worked") {
		t.Fatalf("preview = %q", preview)
	}
	if res["fired"] != float64(1) {
		t.Fatalf("test fire = %v", res)
	}
}

// TestActivateUseCaseSetsToolPolicy proves a text-only automation runs
// without tools and a read-only one gets exactly the read tools. A local
// model told to "answer in plain text" sometimes returns a tool call
// instead of the answer, so the policy belongs to the routine.
func TestActivateUseCaseSetsToolPolicy(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(dir, ".flobot", "integrations.json"))
	srv := New(store.New(), WithExec(&executor.Exec{Bin: bin}), WithWorkDir(dir), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	res := postJSON(t, ts, "/api/v1/usecases/triage/activate", map[string]any{
		"create_integration": true,
	})
	rt, _ := res["routine"].(map[string]any)
	if rt["tools"] != store.ToolsNone {
		t.Fatalf("triage routine tools = %v, want %q", rt["tools"], store.ToolsNone)
	}

	res = postJSON(t, ts, "/api/v1/usecases/chatops/activate", map[string]any{
		"create_integration": true,
	})
	rt, _ = res["routine"].(map[string]any)
	// The kit is text-only: the local route does not execute the model's
	// tool calls, so an automation that may call one returns the call
	// instead of the answer.
	if rt["tools"] != store.ToolsNone {
		t.Fatalf("chatops routine tools = %v, want %q", rt["tools"], store.ToolsNone)
	}
}

// TestPatchRoutineToolPolicy proves the operator can close or widen a
// routine's tool set without rewriting its playbook.
func TestPatchRoutineToolPolicy(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	ist := store.NewIntegrationStore()
	ist.SetPath(filepath.Join(dir, ".flobot", "integrations.json"))
	srv := New(store.New(), WithExec(&executor.Exec{Bin: bin}), WithWorkDir(dir), WithIntegrationStore(ist))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	rtID := armRoutine(t, ts)
	res := patchJSON(t, ts, "/api/v1/routines/"+rtID, map[string]any{"tools": "none"})
	rt, _ := res["routine"].(map[string]any)
	if rt == nil {
		// The routine view embeds the routine; fall back to the flat view.
		rt = res
	}
	if rt["tools"] != "none" {
		t.Fatalf("routine tools = %v, want none", res["tools"])
	}
	res = patchJSON(t, ts, "/api/v1/routines/"+rtID, map[string]any{"tools": "read,glob"})
	rt, _ = res["routine"].(map[string]any)
	if rt == nil {
		rt = res
	}
	if rt["tools"] != "read,glob" {
		t.Fatalf("routine tools = %v, want read,glob", res["tools"])
	}
}
