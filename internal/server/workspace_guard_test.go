package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkspaceRefusalReason mirrors flocode's own workspace safety gate.
// flobot spawns flocode with cwd = the workspace, so a workspace flocode
// would refuse must be refused here too — with a reason, not a mystery at
// spawn time.
func TestWorkspaceRefusalReason(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	// A normal project folder is fine.
	safe := filepath.Join(home, "dev", "some-project")
	if reason := WorkspaceRefusalReason(safe); reason != "" {
		t.Fatalf("safe workspace refused: %s", reason)
	}
	// A subdirectory of a temporary directory is fine too: flocode refuses
	// the temporary directory itself, not everything under it.
	tmp := os.TempDir()
	if reason := WorkspaceRefusalReason(filepath.Join(tmp, "project")); reason != "" {
		t.Fatalf("subdirectory of a temp dir refused: %s", reason)
	}

	cases := []struct {
		name string
		path string
	}{
		{"home", home},
		{"root", "/"},
		{"tmp", "/tmp"},
		{"private tmp", "/private/tmp"},
		{"var tmp", "/var/tmp"},
		{"os temp dir", tmp},
		{"etc", "/etc"},
		{"usr", "/usr"},
		{"library", "/Library"},
		{"inside git", filepath.Join(home, "dev", "repo", ".git", "objects")},
		{"empty", ""},
	}
	for _, c := range cases {
		if reason := WorkspaceRefusalReason(c.path); reason == "" {
			t.Errorf("%s (%q) was accepted, want a refusal", c.name, c.path)
		}
	}
}

// TestWorkspaceSelectRefusesAnUnusableFolder proves the refusal reaches the
// user at selection time, instead of surfacing later as a failed run.
func TestWorkspaceSelectRefusesAnUnusableFolder(t *testing.T) {
	home := t.TempDir()
	start := t.TempDir()
	srv := New(nil, WithHome(home), WithWorkDir(start))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// A folder that IS the temporary directory is refused by name.
	bad := os.TempDir()
	res := postJSONStatus(t, ts, "/api/v1/workspace/select", map[string]any{"path": bad})
	if res.code != 400 {
		t.Fatalf("status = %d, want 400 (%s)", res.code, res.body)
	}
	if !strings.Contains(res.body, "temporary directory") {
		t.Fatalf("body = %q, want a temporary-directory reason", res.body)
	}

	// A normal folder is still accepted.
	ok := filepath.Join(start, "project")
	if err := os.MkdirAll(ok, 0o755); err != nil {
		t.Fatal(err)
	}
	res = postJSONStatus(t, ts, "/api/v1/workspace/select", map[string]any{"path": ok})
	if res.code != 200 {
		t.Fatalf("status = %d, want 200 (%s)", res.code, res.body)
	}
}
