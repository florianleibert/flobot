package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/florianleibert/flobot/internal/routes"
)

// TestStatePathDefault checks the default location is ~/.flobot/state.json.
// FLOBOT_TEST_STATE (set by TestMain) must be cleared for this test.
func TestStatePathDefault(t *testing.T) {
	t.Setenv("FLOBOT_TEST_STATE", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	st := NewState()
	want := filepath.Join(home, ".flobot", "state.json")
	if got := st.Path(); got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

// TestStateSavePermissionsAndNoLeaks checks the file is written 0600 and
// contains no Owner field or secret-looking values.
func TestStateSavePermissionsAndNoLeaks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	st := NewState()
	st.SetPath(path)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("state file perms = %o, want 0600", got)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	low := strings.ToLower(string(raw))
	for _, leak := range []string{`"owner"`, "llm_", "api_key", "secret"} {
		if strings.Contains(low, leak) {
			t.Fatalf("state file leaked %q: %s", leak, raw)
		}
	}
}

// TestStateInitializesPinnedDefaults checks that a missing file seeds the
// roster with glm, qwen and muse, each pinned to its model.
func TestStateInitializesPinnedDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	st := NewState()
	st.SetPath(path)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}

	bots := st.ListBots(ActorLocal)
	if len(bots) != 3 {
		t.Fatalf("default bots = %d, want 3: %+v", len(bots), bots)
	}
	byName := map[string]Bot{}
	for _, b := range bots {
		byName[b.Name] = b
	}
	want := map[string]string{
		"glm":  "glm-5.2",
		"qwen": "qwen-3.8",
		"muse": "muse-spark-1.3",
	}
	for name, pin := range want {
		b, ok := byName[name]
		if !ok {
			t.Fatalf("missing default bot %q: %+v", name, bots)
		}
		if b.Route != name {
			t.Fatalf("bot %s route = %q", name, b.Route)
		}
		gotPin, err := routes.ResolvePin(b.Route)
		if err != nil || gotPin != pin {
			t.Fatalf("bot %s pin = %q %v, want %q", name, gotPin, err, pin)
		}
	}

	// The persisted file must contain the three pinned teammates.
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"glm", "qwen", "muse"} {
		if !strings.Contains(string(raw), name) {
			t.Fatalf("state file missing teammate %q: %s", name, raw)
		}
	}
}

// TestStateLocalPinRoundTrip proves the local route pin survives a
// save/load cycle, so pinning local is not lost on restart.
func TestStateLocalPinRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	st := NewState()
	st.SetPath(path)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	routes.SetLocalPin("qwen3-coder-30b-a3b-mlx")
	defer routes.SetLocalPin("")
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	loaded := NewState()
	loaded.SetPath(path)
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	if got := routes.LocalPin(); got != "qwen3-coder-30b-a3b-mlx" {
		t.Fatalf("local pin after load = %q, want qwen3-coder-30b-a3b-mlx", got)
	}
}

// TestStateLoadRoundTrip checks that a saved roster loads back intact.
func TestStateLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	st := NewState()
	st.SetPath(path)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	// Add a user bot on top of the defaults.
	st2 := New()
	st2.SetPersistPath(path)
	bot, err := st2.CreateBot(ActorLocal, "Extra", "coder", "qwen")
	if err != nil {
		t.Fatal(err)
	}
	if err := st2.Save(); err != nil {
		t.Fatal(err)
	}

	loaded := NewState()
	loaded.SetPath(path)
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	bots := loaded.ListBots(ActorLocal)
	if len(bots) != 4 {
		t.Fatalf("loaded bots = %d, want 4: %+v", len(bots), bots)
	}
	found := false
	for _, b := range bots {
		if b.ID == bot.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("user bot %s not loaded: %+v", bot.ID, bots)
	}
}

// TestStateCorruptedJSONIsGraceful checks that a corrupted state file does
// not crash and falls back to the pinned defaults.
func TestStateCorruptedJSONIsGraceful(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	st := NewState()
	st.SetPath(path)
	err := st.Load()
	if err == nil {
		t.Fatal("corrupted state must surface an error for logging")
	}
	// Must not crash and must fall back to the pinned defaults.
	bots := st.ListBots(ActorLocal)
	if len(bots) != 3 {
		t.Fatalf("after corruption, bots = %d, want 3 defaults: %+v", len(bots), bots)
	}

	// A valid save after corruption must succeed and produce clean JSON.
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var check map[string]json.RawMessage
	if err := json.Unmarshal(raw, &check); err != nil {
		t.Fatalf("re-saved state is not valid JSON: %v", err)
	}
}

// TestStateStripsOwnerAndSecrets checks that Owner is never serialized and
// that a record carrying a secret-looking value is refused.
func TestStateStripsOwnerAndSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	st := NewState()
	st.SetPath(path)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	low := strings.ToLower(string(raw))
	if strings.Contains(low, `"owner"`) {
		t.Fatalf("state file contains an Owner field: %s", raw)
	}

	// A record whose serialized form looks like a secret must be refused.
	st.bots["bot-x"] = Bot{ID: "bot-x", Name: "x", Route: "glm", Owner: ActorLocal, Memory: "api_key=abc"}
	if err := st.Save(); err == nil {
		t.Fatal("save with a secret-looking value must be refused")
	}
}
