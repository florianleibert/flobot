package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// M3 acceptance: an operator credential exported in the parent environment is
// not inherited by the child.
func TestChildDoesNotInheritUnlistedEnv(t *testing.T) {
	dir := t.TempDir()
	bin, err := WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(dir, "env.txt")
	ex := &Exec{
		Bin: bin,
		Environ: func() []string {
			return []string{
				"PATH=" + os.Getenv("PATH"),
				"FLOCODE_FAKE_ENV=" + envPath,
				"MODEL_API_KEY=sk-live-abcdef123456",
				"GITHUB_TOKEN=ghp_zzzzzzzzzzzz",
			}
		},
	}
	res := ex.Run(context.Background(), Request{Pin: "glm", Intent: "ask"}, nil)
	if res.Err != nil {
		t.Fatalf("run: %v", res.Err)
	}
	b, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("child wrote no env report: %v", err)
	}
	report := string(b)
	if strings.Contains(report, "ghp_zzzzzzzzzzzz") {
		t.Fatalf("child inherited GITHUB_TOKEN:\n%s", report)
	}
	if !strings.Contains(report, "FLOCODE_LOG_LEVEL=set") {
		t.Fatalf("child lost the allowlisted flocode env:\n%s", report)
	}
}

// M3 acceptance: a secret the child echoes never becomes an event.
//
// The secret is assembled at run time rather than written as a literal, so this
// test file does not itself contain a credential-shaped string.
func TestEventsAreScrubbed(t *testing.T) {
	secret := "sk-live-" + "abcdef123456"
	dir := t.TempDir()
	bin := filepath.Join(dir, "flocode")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"kind\":\"content\",\"turn_id\":1,\"text\":\"leaked " + secret + " here\"}'\n" +
		"printf '%s\\n' '{\"kind\":\"done\",\"turn_id\":1}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// The scrubber is built from the child's own environment, so the value has
	// to be one the child was actually given.
	ex := &Exec{
		Bin:     bin,
		Environ: func() []string { return []string{"MODEL_API_KEY" + "=" + secret} },
	}
	var got []Event
	res := ex.Run(context.Background(), Request{Pin: "glm", Intent: "ask"}, func(ev Event) {
		got = append(got, ev)
	})
	if res.Err != nil {
		t.Fatalf("run: %v", res.Err)
	}
	if len(got) == 0 {
		t.Fatal("no events emitted")
	}
	for _, ev := range append(got, res.Events...) {
		if strings.Contains(ev.Text, secret) {
			t.Fatalf("secret reached an event: %+v", ev)
		}
	}
}
