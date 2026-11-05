package sandbox

import (
	"strings"
	"testing"
)

// secretEnv is the shape of a real environment: a few names a run needs and
// several credentials it must never see.
func secretEnv() []string {
	return []string{
		"PATH=/usr/bin",
		"HOME=/Users/me",
		"TMPDIR=/tmp",
		"MODEL_API_KEY=sk-live-abcdef123456",
		"GITHUB_TOKEN=ghp_zzzzzzzzzzzz",
		"AWS_SECRET_ACCESS_KEY=aws-secret-value-1",
		"SOME_RANDOM_THING=1",
	}
}

func TestAllowEnvKeepsOnlyAllowlistedNames(t *testing.T) {
	got := AllowEnv(secretEnv(), nil)
	joined := strings.Join(got, "\n")
	for _, want := range []string{"PATH=/usr/bin", "HOME=/Users/me", "TMPDIR=/tmp"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("allowlist dropped %q:\n%s", want, joined)
		}
	}
	for _, bad := range []string{"MODEL_API_KEY", "GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY", "SOME_RANDOM_THING"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("allowlist kept %q:\n%s", bad, joined)
		}
	}
}

func TestAllowEnvKeepsFlocodePrefix(t *testing.T) {
	got := AllowEnv([]string{"FLOCODE_LOG_LEVEL=debug", "FLOCODE_HOME=/h"}, nil)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestAllowEnvExtraWins(t *testing.T) {
	got := AllowEnv([]string{"PATH=/usr/bin"}, []string{"PATH=/opt/bin", "FLOCODE_FAKE_ARGV=/tmp/a"})
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	// Extras are appended, so exec's last-wins rule lets them override.
	if got[0] != "PATH=/usr/bin" || got[1] != "PATH=/opt/bin" {
		t.Fatalf("extra must be appended after the inherited value: %v", got)
	}
}

func TestAllowEnvDropsMalformed(t *testing.T) {
	if got := AllowEnv([]string{"NOEQUALS", "=novalue", "PATH=/usr/bin"}, nil); len(got) != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestDroppedEnvNamesWhatWasWithheld(t *testing.T) {
	dropped := DroppedEnv(secretEnv())
	if len(dropped) != 4 {
		t.Fatalf("dropped = %v", dropped)
	}
	if dropped[0] != "AWS_SECRET_ACCESS_KEY" || dropped[1] != "GITHUB_TOKEN" {
		t.Fatalf("dropped is not sorted: %v", dropped)
	}
}
func TestScrubberReplacesSecretValues(t *testing.T) {
	s := NewScrubber("sk-live-abcdef123456")
	if s.Len() != 1 {
		t.Fatalf("Len = %d", s.Len())
	}
	out := s.Scrub("calling with sk-live-abcdef123456 now")
	if strings.Contains(out, "sk-live-abcdef123456") {
		t.Fatalf("secret survived: %q", out)
	}
	if !strings.Contains(out, Redacted) {
		t.Fatalf("no redaction marker: %q", out)
	}
}

func TestScrubberIgnoresShortValues(t *testing.T) {
	s := NewScrubber("short")
	if s.Len() != 0 {
		t.Fatalf("short value should be ignored, Len = %d", s.Len())
	}
	if got := s.Scrub("a short word"); got != "a short word" {
		t.Fatalf("text was mangled: %q", got)
	}
}

func TestScrubberLongestFirst(t *testing.T) {
	long := "sk-live-abcdef123456"
	short := "abcdef123456"
	s := NewScrubber(short, long)
	out := s.Scrub("value " + long)
	if strings.Contains(out, short) {
		t.Fatalf("shorter token left a fragment: %q", out)
	}
}

func TestScrubberNilIsNoOp(t *testing.T) {
	var s *Scrubber
	if got := s.Scrub("anything"); got != "anything" {
		t.Fatalf("nil scrubber changed text: %q", got)
	}
	if s.Len() != 0 {
		t.Fatal("nil Len should be 0")
	}
}

func TestScrubEnvValuesPicksSecretNames(t *testing.T) {
	s := ScrubEnvValues(secretEnv())
	if s.Len() != 3 {
		t.Fatalf("Len = %d, want 3", s.Len())
	}
	out := s.Scrub("key sk-live-abcdef123456 and token ghp_zzzzzzzzzzzz")
	if strings.Contains(out, "sk-live") || strings.Contains(out, "ghp_") {
		t.Fatalf("env secrets survived: %q", out)
	}
}

func TestLooksSecret(t *testing.T) {
	for _, name := range []string{"MODEL_API_KEY", "GH_TOKEN", "db_password", "AWS_CREDENTIALS"} {
		if !looksSecret(name) {
			t.Fatalf("%q should look secret", name)
		}
	}
	for _, name := range []string{"PATH", "HOME", "LANG"} {
		if looksSecret(name) {
			t.Fatalf("%q should not look secret", name)
		}
	}
}
