package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeWSFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func newWS(t *testing.T) (*Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return w, dir
}

func TestDescriptorOpenReadWrite(t *testing.T) {
	w, dir := newWS(t)
	writeWSFile(t, dir, "main.go", "package main\n")

	d, err := w.Open("main.go")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if d.ID <= 0 {
		t.Errorf("descriptor ID = %d, want > 0", d.ID)
	}
	if d.Path != "main.go" {
		t.Errorf("Path = %q, want %q", d.Path, "main.go")
	}
	if d.SHA256 == "" {
		t.Error("SHA256 empty")
	}

	f, err := w.ReadDescriptor(d.ID)
	if err != nil {
		t.Fatalf("ReadDescriptor: %v", err)
	}
	if f.Content != "package main\n" {
		t.Errorf("content = %q", f.Content)
	}
	if !f.Exists {
		t.Error("Exists = false")
	}

	out, err := w.WriteDescriptor(d.ID, "package main\n\nfunc main() {}\n")
	if err != nil {
		t.Fatalf("WriteDescriptor: %v", err)
	}
	if out.SHA256 == d.SHA256 {
		t.Error("SHA256 unchanged after write")
	}
	onDisk, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("read disk: %v", err)
	}
	if string(onDisk) != "package main\n\nfunc main() {}\n" {
		t.Errorf("on disk = %q", onDisk)
	}
}

func TestDescriptorWriteIsIdempotent(t *testing.T) {
	w, dir := newWS(t)
	writeWSFile(t, dir, "a.txt", "one")
	d, err := w.Open("a.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// A second save through the same descriptor must not be a conflict: the
	// descriptor's own write updated the tracked hash.
	if _, err := w.WriteDescriptor(d.ID, "two"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := w.WriteDescriptor(d.ID, "three"); err != nil {
		t.Fatalf("second write: %v", err)
	}
	onDisk, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(onDisk) != "three" {
		t.Errorf("on disk = %q, want %q", onDisk, "three")
	}
}

func TestDescriptorWriteConflictsOnExternalChange(t *testing.T) {
	w, dir := newWS(t)
	writeWSFile(t, dir, "a.txt", "one")
	d, err := w.Open("a.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Someone else (a flocode run, another tab) edits the file.
	writeWSFile(t, dir, "a.txt", "external edit")
	if _, err := w.WriteDescriptor(d.ID, "mine"); !errors.Is(err, ErrShaMismatch) {
		t.Errorf("WriteDescriptor = %v, want ErrShaMismatch", err)
	}
	onDisk, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(onDisk) != "external edit" {
		t.Errorf("external edit lost: %q", onDisk)
	}
}

func TestDescriptorRejectsEscapesAndProtectedPaths(t *testing.T) {
	w, dir := newWS(t)
	writeWSFile(t, dir, "ok.txt", "x")
	if _, err := w.Open("../escape.txt"); err == nil {
		t.Error("Open(../escape.txt): want error")
	}
	if _, err := w.Open("/etc/hosts"); err == nil {
		t.Error("Open(/etc/hosts): want error")
	}
	if _, err := w.Open(".git/config"); !errors.Is(err, ErrProtected) {
		t.Errorf("Open(.git/config) = %v, want ErrProtected", err)
	}
	_ = dir
}

func TestDescriptorRejectsSymlinkEscape(t *testing.T) {
	w, dir := newWS(t)
	target := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if _, err := w.Open("link.txt"); !errors.Is(err, ErrNotInWorkspace) {
		t.Errorf("Open(symlink) = %v, want ErrNotInWorkspace", err)
	}
}

func TestDescriptorLifecycle(t *testing.T) {
	w, dir := newWS(t)
	writeWSFile(t, dir, "a.txt", "a")
	d, err := w.Open("a.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := len(w.Descriptors()); got != 1 {
		t.Errorf("Descriptors() len = %d, want 1", got)
	}
	if _, err := w.Descriptor(d.ID); err != nil {
		t.Fatalf("Descriptor: %v", err)
	}
	if err := w.CloseDescriptor(d.ID); err != nil {
		t.Fatalf("CloseDescriptor: %v", err)
	}
	if _, err := w.ReadDescriptor(d.ID); !errors.Is(err, ErrUnknownDescriptor) {
		t.Errorf("ReadDescriptor after close = %v, want ErrUnknownDescriptor", err)
	}
	if err := w.CloseDescriptor(d.ID); !errors.Is(err, ErrUnknownDescriptor) {
		t.Errorf("CloseDescriptor twice = %v, want ErrUnknownDescriptor", err)
	}
	if got := len(w.Descriptors()); got != 0 {
		t.Errorf("Descriptors() len = %d, want 0", got)
	}
}

func TestDescriptorLimit(t *testing.T) {
	w, dir := newWS(t)
	for i := 0; i < MaxDescriptors+1; i++ {
		writeWSFile(t, dir, filepath.Join("f", string(rune('a'+i%26))+itoa(i)), "x")
	}
	for i := 0; i < MaxDescriptors; i++ {
		if _, err := w.Open(filepath.Join("f", string(rune('a'+i%26))+itoa(i))); err != nil {
			t.Fatalf("Open %d: %v", i, err)
		}
	}
	if _, err := w.Open(filepath.Join("f", "zzz")); !errors.Is(err, ErrTooManyDescriptors) {
		t.Errorf("Open over limit = %v, want ErrTooManyDescriptors", err)
	}
}

func TestCloseAllDescriptors(t *testing.T) {
	w, dir := newWS(t)
	writeWSFile(t, dir, "a.txt", "a")
	writeWSFile(t, dir, "b.txt", "b")
	if _, err := w.Open("a.txt"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := w.Open("b.txt"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	w.CloseAllDescriptors()
	if got := len(w.Descriptors()); got != 0 {
		t.Errorf("Descriptors() len = %d, want 0", got)
	}
}

// itoa is a tiny local helper so the test does not pull in strconv for one
// call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
