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
)

func TestWorkspaceSelectRuntimeSwitch(t *testing.T) {
	home := t.TempDir()
	if p, err := filepath.EvalSymlinks(home); err == nil {
		home = p
	}
	start := t.TempDir()
	if p, err := filepath.EvalSymlinks(start); err == nil {
		start = p
	}
	target := filepath.Join(start, "tictactoe")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}

	srv := New(nil, WithHome(home), WithWorkDir(start))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// Refuse the workspace package's own roots: filesystem root and home.
	sysHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if p, err := filepath.EvalSymlinks(sysHome); err == nil {
		sysHome = p
	}
	for _, bad := range []string{"relative/dir", "/", sysHome} {
		res := postJSONStatus(t, ts, "/api/v1/workspace/select", map[string]any{"path": bad})
		if res.code != 400 {
			t.Fatalf("bad path %q: status %d, want 400 (%s)", bad, res.code, res.body)
		}
	}
	// Missing folders outside the default workspace root are a 404.
	res := postJSONStatus(t, ts, "/api/v1/workspace/select", map[string]any{"path": filepath.Join(start, "nope", "deep")})
	if res.code != 404 {
		t.Fatalf("missing folder: status %d, want 404 (%s)", res.code, res.body)
	}

	// Switch to an existing folder.
	res = postJSONStatus(t, ts, "/api/v1/workspace/select", map[string]any{"path": target})
	if res.code != 200 {
		t.Fatalf("select: %d %s", res.code, res.body)
	}
	var sel map[string]any
	if err := json.Unmarshal([]byte(res.body), &sel); err != nil {
		t.Fatal(err)
	}
	if sel["root"] != target {
		t.Fatalf("root = %v, want %v", sel["root"], target)
	}

	// Editor root now reports the new folder...
	root := getJSON(t, ts, "/api/v1/workspace/root")
	if root["root"] != target {
		t.Fatalf("workspace/root = %v, want %v", root["root"], target)
	}
	// ...and the file API is relative to it.
	res = postJSONStatus(t, ts, "/api/v1/workspace/file?path=index.html", map[string]any{"content": "<h1>ttt</h1>"})
	if res.code >= 300 {
		t.Fatalf("create file: %d %s", res.code, res.body)
	}
	resp, gerr := http.Get(ts.URL + "/api/v1/workspace/file?path=index.html")
	if gerr != nil {
		t.Fatal(gerr)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), "ttt") {
		t.Fatalf("file body = %s", b)
	}
	if _, err := os.Stat(filepath.Join(target, "index.html")); err != nil {
		t.Fatalf("file not written under selected folder: %v", err)
	}

	// Recents persist so a relaunch reopens the folder. The exported
	// helper reads the real ~/.flobot/config.json, so assert through the
	// test home instead (LatestWorkspace itself is trivially covered by
	// the same code path once the file exists).
	cfgPath := filepath.Join(home, ".flobot", "config.json")
	cb, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config not persisted: %v", err)
	}
	var cfg workspaceConfig
	if err := json.Unmarshal(cb, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspaces) == 0 || cfg.Workspaces[0] != target {
		t.Fatalf("config workspaces = %v, want %v first", cfg.Workspaces, target)
	}
	roots := getJSON(t, ts, "/api/v1/workspace/roots")
	list, _ := roots["recent"].([]any)
	if len(list) == 0 || list[0] != target {
		t.Fatalf("recent = %v, want %v first", roots["recent"], target)
	}
}

func TestRunFailureHint(t *testing.T) {
	// Bare exit status with a 401 in stderr explains itself.
	if h := runFailureHint("exit status 1", "openai: HTTP 401: len=104"); !strings.Contains(h, "401") {
		t.Errorf("401 hint = %q", h)
	}
	// Missing key file.
	if h := runFailureHint("exit status 1", `model profile "x" api_key references MODEL_API_KEY, but that environment variable is not set`); !strings.Contains(h, "MODEL_API_KEY") {
		t.Errorf("key hint = %q", h)
	}
	// Non-git workspace.
	if h := runFailureHint("exit status 1", "WARNING: The directory /x is not a Git repository."); !strings.Contains(h, "git repository") {
		t.Errorf("git hint = %q", h)
	}
	// Unrecognized exit: fall back to stderr tail.
	if h := runFailureHint("exit status 1", "something odd happened"); !strings.Contains(h, "stderr:") {
		t.Errorf("fallback hint = %q", h)
	}
	// No stderr, nothing mapped: leave the raw error alone.
	if h := runFailureHint("exit status 2", ""); h != "" {
		t.Errorf("unexpected hint %q", h)
	}
	// A non-exit error never gets a hint.
	if h := runFailureHint("context canceled", "not a Git repository"); h != "" {
		t.Errorf("non-exit error should not hint, got %q", h)
	}
}

// TestWorkspaceTreeNoiseFlag proves the 👁 toggle has a server-side
// counterpart: dependency/build artifact directories are hidden by default
// and listed when noise=1 is requested. It also proves the workspace API
// can create a file inside directories that do not exist yet.
func TestWorkspaceTreeNoiseFlag(t *testing.T) {
	home := t.TempDir()
	start := t.TempDir()
	if p, err := filepath.EvalSymlinks(start); err == nil {
		start = p
	}
	if err := os.MkdirAll(filepath.Join(start, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(start, "src"), 0o755); err != nil {
		t.Fatal(err)
	}

	srv := New(nil, WithHome(home), WithWorkDir(start))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	names := func(tree map[string]any) []string {
		var out []string
		ents, _ := tree["entries"].([]any)
		for _, e := range ents {
			m, _ := e.(map[string]any)
			if n, ok := m["name"].(string); ok {
				out = append(out, n)
			}
		}
		return out
	}

	tree := getJSON(t, ts, "/api/v1/workspace/tree?dir=.")
	for _, n := range names(tree) {
		if n == "node_modules" {
			t.Fatalf("node_modules visible by default: %v", names(tree))
		}
	}
	if hidden, _ := tree["hidden"].(float64); hidden != 1 {
		t.Fatalf("hidden = %v, want 1", tree["hidden"])
	}

	tree = getJSON(t, ts, "/api/v1/workspace/tree?dir=.&noise=1")
	found := false
	for _, n := range names(tree) {
		if n == "node_modules" {
			found = true
		}
	}
	if !found {
		t.Fatalf("node_modules missing with noise=1: %v", names(tree))
	}

	// A file can be created inside directories that do not exist yet.
	res := postJSONStatus(t, ts, "/api/v1/workspace/file?path=a/b/c.js", map[string]any{"content": "hi"})
	if res.code != 201 {
		t.Fatalf("nested create: %d %s", res.code, res.body)
	}
	if _, err := os.Stat(filepath.Join(start, "a", "b", "c.js")); err != nil {
		t.Fatalf("nested file not written: %v", err)
	}
}
