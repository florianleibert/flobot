package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestWS(t *testing.T) *Workspace {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestValidateRejectsTraversal(t *testing.T) {
	ws := newTestWS(t)
	bad := []string{
		"",
		"../",
		"../a.txt",
		"sub/../../a.txt",
		"/etc/passwd",
		"a\x00b",
		"..",
		"a/../../b",
	}
	for _, p := range bad {
		if _, err := ws.validate(p); err == nil {
			t.Errorf("validate(%q) accepted, want reject", p)
		}
	}
	if _, err := ws.validate("a.txt"); err != nil {
		t.Errorf("validate(a.txt): %v", err)
	}
}

func TestValidateSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	ws, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.validate("link"); !errors.Is(err, ErrNotInWorkspace) {
		t.Errorf("symlink escape: got %v, want ErrNotInWorkspace", err)
	}
	if _, err := ws.validate("link/secret.txt"); !errors.Is(err, ErrNotInWorkspace) {
		t.Errorf("symlink child: got %v, want ErrNotInWorkspace", err)
	}
}

func TestTreeSkipsHidden(t *testing.T) {
	ws := newTestWS(t)
	entries, err := ws.Tree(".", false)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "sub") || !strings.Contains(joined, "a.txt") {
		t.Errorf("tree missing entries: %v", names)
	}
	// all=true includes dotfiles
	all, err := ws.Tree(".", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range all {
		if e.Name == "node_modules" {
			t.Error("node_modules should always be skipped")
		}
	}
}

func TestReadWriteRoundTrip(t *testing.T) {
	ws := newTestWS(t)
	f, err := ws.Read("a.txt")
	if err != nil || !f.Exists || f.Content != "alpha" {
		t.Fatalf("read: %+v err=%v", f, err)
	}
	up, err := ws.Write("a.txt", "alpha2", f.SHA256)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if up.SHA256 == f.SHA256 {
		t.Error("sha unchanged after write")
	}
	again, err := ws.Read("a.txt")
	if err != nil || again.Content != "alpha2" {
		t.Fatalf("reread: %+v err=%v", again, err)
	}
}

func TestWriteShaMismatch(t *testing.T) {
	ws := newTestWS(t)
	if _, err := ws.Write("a.txt", "x", "deadbeef"); !errors.Is(err, ErrShaMismatch) {
		t.Errorf("stale sha: got %v, want ErrShaMismatch", err)
	}
	if _, err := ws.Write("a.txt", "x", ""); !errors.Is(err, ErrShaMismatch) {
		t.Errorf("no sha on existing: got %v, want ErrShaMismatch", err)
	}
}

func TestCreateDeleteRename(t *testing.T) {
	ws := newTestWS(t)
	if _, err := ws.Create("new.txt", "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Create("new.txt", "n2"); !errors.Is(err, ErrExists) {
		t.Errorf("create over existing: %v", err)
	}
	if err := ws.Rename("new.txt", "sub/renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Read("new.txt"); err != nil || false {
		t.Fatal(err)
	}
	f, _ := ws.Read("new.txt")
	if f.Exists {
		t.Error("renamed file still readable at old path")
	}
	if err := ws.Delete("sub/renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if err := ws.Delete("sub/renamed.txt"); err != nil {
		t.Errorf("delete missing should be no-op: %v", err)
	}
}

func TestProtectedGit(t *testing.T) {
	ws := newTestWS(t)
	if _, err := ws.Write(".git/config", "x", ""); !errors.Is(err, ErrProtected) {
		t.Errorf("write .git: %v", err)
	}
	if err := ws.Delete(".git/HEAD"); !errors.Is(err, ErrProtected) {
		t.Errorf("delete .git: %v", err)
	}
	if err := ws.Rename(".git", "git2"); !errors.Is(err, ErrProtected) {
		t.Errorf("rename .git: %v", err)
	}
}

func TestBinaryAndCaps(t *testing.T) {
	dir := t.TempDir()
	ws, _ := New(dir)
	if err := os.WriteFile(filepath.Join(dir, "bin"), []byte{0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := ws.Read("bin")
	if err != nil || !f.Binary || f.Content != "" {
		t.Fatalf("binary: %+v err=%v", f, err)
	}
	big := make([]byte, MaxFileBytes+1)
	if err := os.WriteFile(filepath.Join(dir, "big"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Read("big"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversize read: %v", err)
	}
	if _, err := ws.Write("big2", string(big), ""); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversize write: %v", err)
	}
}

func TestMissingFileExistsFalse(t *testing.T) {
	ws := newTestWS(t)
	f, err := ws.Read("nope.txt")
	if err != nil || f.Exists {
		t.Fatalf("missing: %+v err=%v", f, err)
	}
}
