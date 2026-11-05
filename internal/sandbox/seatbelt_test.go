package sandbox

import (
	"runtime"
	"strings"
	"testing"
)

func TestProfileDeniesByDefault(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Seatbelt is macOS-only")
	}
	p := Profile(Policy{WorkDir: "/tmp/work"})
	if p == "" {
		t.Fatal("no profile generated")
	}
	if !strings.Contains(p, "(deny default)") {
		t.Fatalf("profile must deny by default:\n%s", p)
	}
	if strings.Contains(p, "(allow network*)") {
		t.Fatalf("network allowed by default:\n%s", p)
	}
	if !strings.Contains(p, "/tmp/work") {
		t.Fatalf("work dir not writable:\n%s", p)
	}
}

func TestProfileOptIns(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Seatbelt is macOS-only")
	}
	p := Profile(Policy{WorkDir: "/tmp/work", AllowNetwork: true, ReadHome: true})
	if !strings.Contains(p, "(allow network*)") {
		t.Fatalf("network opt-in missing:\n%s", p)
	}
	if !strings.Contains(p, "(subpath \"/Users\")") {
		t.Fatalf("home read opt-in missing:\n%s", p)
	}
}

func TestProfileRequiresWorkDir(t *testing.T) {
	if got := Profile(Policy{}); got != "" {
		t.Fatalf("a profile with no writable dir must not be generated: %q", got)
	}
}

func TestCommandWrapsOnlyWhenConfined(t *testing.T) {
	argv := []string{"flocode", "-p", "hi"}
	wrapped := Command(Policy{WorkDir: "/tmp/work"}, argv)
	if runtime.GOOS != "darwin" {
		if len(wrapped) != len(argv) {
			t.Fatalf("off darwin argv must be unchanged: %v", wrapped)
		}
		return
	}
	if len(wrapped) <= len(argv) || wrapped[0] != "sandbox-exec" {
		t.Fatalf("argv not wrapped: %v", wrapped)
	}
	if wrapped[len(wrapped)-1] != "hi" {
		t.Fatalf("original argv lost: %v", wrapped)
	}
}
