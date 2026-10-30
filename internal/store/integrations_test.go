package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegrationStoreCreateListDelete(t *testing.T) {
	dir := t.TempDir()
	ts := NewIntegrationStore()
	ts.SetPath(filepath.Join(dir, ".flobot", "integrations.json"))

	in, err := ts.CreateIntegration(IntegrationGmail, "work inbox", "label: inbox", AuthBearer)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(in.ID, "int-") {
		t.Fatalf("id = %q", in.ID)
	}
	if len(in.Secret) < 32 {
		t.Fatalf("secret too short: %q", in.Secret)
	}

	got, ok := ts.Integration(in.ID)
	if !ok || got.Name != "work inbox" || got.Kind != IntegrationGmail {
		t.Fatalf("integration = %+v", got)
	}

	// The secret is persisted (the file exists to hold it) and the file is
	// 0600.
	raw, err := os.ReadFile(ts.Path())
	if err != nil {
		t.Fatalf("integrations not persisted: %v", err)
	}
	if !strings.Contains(string(raw), in.Secret) {
		t.Fatalf("secret not in file")
	}
	st, err := os.Stat(ts.Path())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", st.Mode().Perm())
	}

	if !ts.DeleteIntegration(in.ID) {
		t.Fatal("delete reported false")
	}
	if _, ok := ts.Integration(in.ID); ok {
		t.Fatal("integration still present after delete")
	}
	if ts.DeleteIntegration(in.ID) {
		t.Fatal("second delete reported true")
	}
}

func TestIntegrationStoreUnknownKind(t *testing.T) {
	ts := NewIntegrationStore()
	if _, err := ts.CreateIntegration("carrier-pigeon", "x", "", ""); err == nil {
		t.Fatal("unknown kind accepted")
	}
	if _, err := ts.CreateIntegration(IntegrationWebhook, "  ", "", ""); err == nil {
		t.Fatal("blank name accepted")
	}
	if _, err := ts.CreateIntegration(IntegrationWebhook, "x", "", "smoke-signals"); err == nil {
		t.Fatal("unknown auth mode accepted")
	}
}

func TestIntegrationStoreAuthModes(t *testing.T) {
	ts := NewIntegrationStore()
	// Every provider kind is accepted, with the scheme it needs.
	for _, kind := range IntegrationKinds {
		in, err := ts.CreateIntegration(kind, "x "+kind, "", "")
		if err != nil {
			t.Fatalf("kind %q: %v", kind, err)
		}
		if in.AuthMode != AuthBearer {
			t.Fatalf("default auth mode for %q = %q", kind, in.AuthMode)
		}
	}
	// The standard-webhooks scheme uses a whsec_ prefixed base64 secret;
	// every other scheme uses hex.
	in, err := ts.CreateIntegration(IntegrationWebhook, "std", "", AuthStandard)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(in.Secret, "whsec_") {
		t.Fatalf("standard secret = %q", in.Secret)
	}
	hexIn, err := ts.CreateIntegration(IntegrationWebhook, "hex", "", AuthHMAC)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(hexIn.Secret, "whsec_") {
		t.Fatalf("hmac secret = %q", hexIn.Secret)
	}
}

func TestIntegrationStoreRotateSecret(t *testing.T) {
	ts := NewIntegrationStore()
	in, err := ts.CreateIntegration(IntegrationWebhook, "hook", "", AuthBearer)
	if err != nil {
		t.Fatal(err)
	}
	rot, ok := ts.RotateSecret(in.ID)
	if !ok {
		t.Fatal("rotate reported missing")
	}
	if rot.Secret == in.Secret {
		t.Fatal("secret unchanged after rotate")
	}
	if !ts.Authenticate(in.ID, rot.Secret) {
		t.Fatal("new secret rejected")
	}
	if ts.Authenticate(in.ID, in.Secret) {
		t.Fatal("old secret still accepted")
	}
	if _, ok := ts.RotateSecret("int-nope"); ok {
		t.Fatal("rotate of unknown integration reported success")
	}
	// The rotation is persisted.
	ts2 := NewIntegrationStore()
	ts2.SetPath(ts.Path())
	if err := ts2.Load(); err != nil {
		t.Fatal(err)
	}
	got, ok := ts2.Integration(in.ID)
	if !ok || got.Secret != rot.Secret {
		t.Fatalf("rotated secret not persisted: %+v", got)
	}
}

func TestSecretHintHidesTheSecret(t *testing.T) {
	in, err := NewIntegrationStore().CreateIntegration(IntegrationWebhook, "hook", "", AuthBearer)
	if err != nil {
		t.Fatal(err)
	}
	hint := SecretHint(in.Secret)
	if strings.Contains(hint, in.Secret) {
		t.Fatalf("hint leaks the secret: %q", hint)
	}
	if !strings.HasSuffix(hint, in.Secret[len(in.Secret)-4:]) {
		t.Fatalf("hint = %q", hint)
	}
}

func TestIntegrationStoreAuthenticate(t *testing.T) {
	ts := NewIntegrationStore()
	in, err := ts.CreateIntegration(IntegrationWebhook, "hook", "", AuthBearer)
	if err != nil {
		t.Fatal(err)
	}
	if !ts.Authenticate(in.ID, in.Secret) {
		t.Fatal("correct secret rejected")
	}
	if ts.Authenticate(in.ID, "wrong") {
		t.Fatal("wrong secret accepted")
	}
	if ts.Authenticate("nope", in.Secret) {
		t.Fatal("unknown id accepted")
	}
}

func TestIntegrationStorePersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".flobot", "integrations.json")

	ts := NewIntegrationStore()
	ts.SetPath(path)
	in, err := ts.CreateIntegration(IntegrationGmail, "persist me", "label: x", AuthBearer)
	if err != nil {
		t.Fatal(err)
	}

	ts2 := NewIntegrationStore()
	ts2.SetPath(path)
	if err := ts2.Load(); err != nil {
		t.Fatal(err)
	}
	got, ok := ts2.Integration(in.ID)
	if !ok {
		t.Fatal("integration lost on reload")
	}
	if got.Secret != in.Secret {
		t.Fatalf("secret changed on reload: %q vs %q", got.Secret, in.Secret)
	}
}

func TestIntegrationStoreRecordEvent(t *testing.T) {
	ts := NewIntegrationStore()
	in, err := ts.CreateIntegration(IntegrationWebhook, "hook", "", AuthBearer)
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.RecordEvent(in, IntegrationEvent{RoutineID: "rtn-x", RunID: "run-y", Status: "fired"}); err != nil {
		t.Fatal(err)
	}
	if err := ts.RecordEvent(in, IntegrationEvent{RoutineID: "rtn-x", RunID: "run-z", Status: "fired"}); err != nil {
		t.Fatal(err)
	}
	evs := ts.ListEvents()
	if len(evs) != 2 {
		t.Fatalf("events = %d, want 2", len(evs))
	}
	if evs[0].RunID != "run-y" || evs[1].RunID != "run-z" {
		t.Fatalf("event order = %+v", evs)
	}
}

func TestIntegrationStoreLeakGuardAllowsSecretOnly(t *testing.T) {
	dir := t.TempDir()
	ts := NewIntegrationStore()
	ts.SetPath(filepath.Join(dir, ".flobot", "integrations.json"))
	if _, err := ts.CreateIntegration(IntegrationWebhook, "hook", "", AuthBearer); err != nil {
		t.Fatal(err)
	}
	// The file must exist and contain the secret field (allowed), but no
	// api_key/llm_ material (still refused).
	raw, err := os.ReadFile(ts.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"secret"`) {
		t.Fatalf("secret field missing: %s", raw)
	}
	if strings.Contains(strings.ToLower(string(raw)), "api_key") {
		t.Fatalf("api_key leaked into file: %s", raw)
	}
}

func TestRoutineBindIntegration(t *testing.T) {
	st := New()
	coder, err := st.CreateBot(ActorLocal, "Coder", "coder", "glm")
	if err != nil {
		t.Fatal(err)
	}
	sk, err := st.CreateSkill(ActorLocal, "s", "how-to")
	if err != nil {
		t.Fatal(err)
	}
	rt, err := st.CreateRoutine(ActorLocal, coder.ID, sk.ID, "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	if rt.IntegrationID != "" {
		t.Fatalf("initial integration = %q", rt.IntegrationID)
	}

	bound, err := st.BindRoutine(rt.ID, ActorLocal, "int-abc")
	if err != nil {
		t.Fatal(err)
	}
	if bound.IntegrationID != "int-abc" {
		t.Fatalf("bound integration = %q", bound.IntegrationID)
	}

	rts := st.RoutinesForIntegration("int-abc")
	if len(rts) != 1 || rts[0].ID != rt.ID {
		t.Fatalf("routines for integration = %+v", rts)
	}

	// Unbind with an empty id.
	unbound, err := st.BindRoutine(rt.ID, ActorLocal, "")
	if err != nil {
		t.Fatal(err)
	}
	if unbound.IntegrationID != "" {
		t.Fatalf("unbound integration = %q", unbound.IntegrationID)
	}
	if len(st.RoutinesForIntegration("int-abc")) != 0 {
		t.Fatal("routine still bound after unbind")
	}

	// A routine created with an integration id carries it.
	rt2, err := st.CreateRoutine(ActorLocal, coder.ID, sk.ID, "manual", "int-abc")
	if err != nil {
		t.Fatal(err)
	}
	if rt2.IntegrationID != "int-abc" {
		t.Fatalf("create-with-integration = %q", rt2.IntegrationID)
	}
}

func TestIntegrationStoreEventAttribution(t *testing.T) {
	ts := NewIntegrationStore()
	a, err := ts.CreateIntegration(IntegrationWebhook, "a", "", AuthBearer)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ts.CreateIntegration(IntegrationWebhook, "b", "", AuthBearer)
	if err != nil {
		t.Fatal(err)
	}
	// An event that fired no routine still belongs to one integration.
	if err := ts.RecordEvent(a, IntegrationEvent{Status: "rejected", Note: "bad credential"}); err != nil {
		t.Fatal(err)
	}
	evs := ts.ListEvents()
	if len(evs) != 1 {
		t.Fatalf("events = %d, want 1", len(evs))
	}
	if evs[0].IntegrationID != a.ID {
		t.Fatalf("integration id = %q, want %q", evs[0].IntegrationID, a.ID)
	}
	if evs[0].IntegrationID == b.ID {
		t.Fatal("event attributed to the wrong integration")
	}
}

func TestRoutineWorktree(t *testing.T) {
	st := New()
	coder, err := st.CreateBot(ActorLocal, "Coder", "coder", "glm")
	if err != nil {
		t.Fatal(err)
	}
	sk, err := st.CreateSkill(ActorLocal, "s", "how-to")
	if err != nil {
		t.Fatal(err)
	}
	rt, err := st.CreateRoutine(ActorLocal, coder.ID, sk.ID, "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	if rt.Worktree != "" {
		t.Fatalf("new routine has a worktree: %q", rt.Worktree)
	}
	// A routine can be pointed at a thread's worktree so its runs do not
	// contend for the shared checkout.
	set, err := st.SetRoutineWorktree(rt.ID, ActorLocal, "thr-1")
	if err != nil {
		t.Fatal(err)
	}
	if set.Worktree != "thr-1" {
		t.Fatalf("worktree = %q", set.Worktree)
	}
	cleared, err := st.SetRoutineWorktree(rt.ID, ActorLocal, "")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Worktree != "" {
		t.Fatalf("worktree = %q after clear", cleared.Worktree)
	}
}
