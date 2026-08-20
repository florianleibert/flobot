package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeVerifyKeyFile writes a Muse key file under home and returns the path.
func writeVerifyKeyFile(t *testing.T, home, content string) string {
	t.Helper()
	dir := filepath.Join(home, ".accounts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "meta.ai")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestVerifyEmitsVerifyKindOnSuccess runs a verify with a valid key and a
// fake flocode that exits 0. It asserts the stream ends with a `verify`
// event (is_error false) and that the child received MODEL_API_KEY.
func TestVerifyEmitsVerifyKindOnSuccess(t *testing.T) {
	dir := t.TempDir()
	bin, err := WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	writeVerifyKeyFile(t, home, testKey+"\n")
	envPath := filepath.Join(dir, "env.txt")
	ex := &Exec{
		Bin:  bin,
		Home: home,
		Environ: func() []string {
			return []string{"FLOCODE_FAKE_ENV=" + envPath}
		},
	}
	var seen []Event
	res := ex.Verify(context.Background(), Request{}, func(ev Event) { seen = append(seen, ev) })
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	// The last event must be the `verify` kind with is_error false.
	last := res.Events[len(res.Events)-1]
	if last.Kind != "verify" {
		t.Fatalf("last event kind = %q, want verify: %#v", last.Kind, res.Events)
	}
	if last.IsError != nil && *last.IsError {
		t.Fatalf("verify event is_error = %v, want false", *last.IsError)
	}
	// The emitted stream must also end with verify.
	if seen[len(seen)-1].Kind != "verify" {
		t.Fatalf("emitted last kind = %q", seen[len(seen)-1].Kind)
	}
	// The child must have received MODEL_API_KEY (presence only).
	dump, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dump), "MODEL_API_KEY=set") {
		t.Fatalf("MODEL_API_KEY not injected: %s", dump)
	}
	// The key value must never appear in events, stderr, or the dump.
	if strings.Contains(string(dump), testKey) {
		t.Fatal("key value leaked into env dump")
	}
	for _, ev := range res.Events {
		if strings.Contains(ev.Text, testKey) || strings.Contains(ev.Tool, testKey) {
			t.Fatalf("key leaked into event: %+v", ev)
		}
	}
	if strings.Contains(res.Stderr, testKey) {
		t.Fatal("key leaked into stderr")
	}
}

// TestVerifyMissingKeyEmitsErr runs a verify with no key file. It asserts
// an `err` event with an actionable message, no `verify` event, and no key
// disclosure.
func TestVerifyMissingKeyEmitsErr(t *testing.T) {
	dir := t.TempDir()
	bin, err := WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir() // no .accounts/meta.ai
	ex := &Exec{
		Bin:  bin,
		Home: home,
		Environ: func() []string {
			return []string{}
		},
	}
	var seen []Event
	res := ex.Verify(context.Background(), Request{}, func(ev Event) { seen = append(seen, ev) })
	if res.Err == nil {
		t.Fatal("expected error for missing key")
	}
	if !errors.Is(res.Err, ErrModelAPIKeyMissing) {
		t.Fatalf("err = %v, want ErrModelAPIKeyMissing", res.Err)
	}
	// The only event must be an `err` with an actionable message.
	if len(res.Events) != 1 {
		t.Fatalf("events = %#v", res.Events)
	}
	ev := res.Events[0]
	if ev.Kind != "err" {
		t.Fatalf("kind = %q, want err", ev.Kind)
	}
	if !strings.Contains(ev.Text, ".accounts/meta.ai") {
		t.Fatalf("err not actionable (no file hint): %q", ev.Text)
	}
	if strings.Contains(ev.Text, testKey) {
		t.Fatalf("err leaks key: %q", ev.Text)
	}
	// No `verify` event may be emitted.
	for _, s := range seen {
		if s.Kind == "verify" {
			t.Fatalf("verify event emitted on missing key: %#v", s)
		}
	}
}

// TestVerifyEmptyKeyEmitsErr runs a verify with an empty key file. It
// asserts an `err` event and no `verify` event.
func TestVerifyEmptyKeyEmitsErr(t *testing.T) {
	dir := t.TempDir()
	bin, err := WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	writeVerifyKeyFile(t, home, "   \n\t\n")
	ex := &Exec{
		Bin:  bin,
		Home: home,
		Environ: func() []string {
			return []string{}
		},
	}
	res := ex.Verify(context.Background(), Request{}, nil)
	if res.Err == nil {
		t.Fatal("expected error for empty key")
	}
	if !errors.Is(res.Err, ErrModelAPIKeyMissing) {
		t.Fatalf("err = %v, want ErrModelAPIKeyMissing", res.Err)
	}
	if len(res.Events) != 1 || res.Events[0].Kind != "err" {
		t.Fatalf("events = %#v", res.Events)
	}
	if !strings.Contains(res.Events[0].Text, ".accounts/meta.ai") {
		t.Fatalf("err not actionable: %q", res.Events[0].Text)
	}
}

// TestVerifyChildFailureEmitsErr runs a verify with a valid key but a
// failing child (simulating an invalid key rejected by the model). It
// asserts an `err` event with an actionable message and no `verify` event.
func TestVerifyChildFailureEmitsErr(t *testing.T) {
	dir := t.TempDir()
	// A fake flocode that exits non-zero.
	bin := filepath.Join(dir, "flocode")
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	writeVerifyKeyFile(t, home, testKey+"\n")
	ex := &Exec{
		Bin:  bin,
		Home: home,
		Environ: func() []string {
			return []string{}
		},
	}
	var seen []Event
	res := ex.Verify(context.Background(), Request{}, func(ev Event) { seen = append(seen, ev) })
	if res.Err == nil {
		t.Fatal("expected error for failing child")
	}
	// The last event must be an `err` with an actionable message.
	last := res.Events[len(res.Events)-1]
	if last.Kind != "err" {
		t.Fatalf("last kind = %q, want err: %#v", last.Kind, res.Events)
	}
	if !strings.Contains(last.Text, "meta.ai") {
		t.Fatalf("err not actionable: %q", last.Text)
	}
	if strings.Contains(last.Text, testKey) {
		t.Fatalf("err leaks key: %q", last.Text)
	}
	// No `verify` event may be emitted.
	for _, s := range seen {
		if s.Kind == "verify" {
			t.Fatalf("verify event emitted on child failure: %#v", s)
		}
	}
}

// TestVerifyInjectsKeyIntoChildEnv asserts the key reaches the child only
// as MODEL_API_KEY and never appears in argv.
func TestVerifyInjectsKeyIntoChildEnv(t *testing.T) {
	dir := t.TempDir()
	bin, err := WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	writeVerifyKeyFile(t, home, testKey+"\n")
	envPath := filepath.Join(dir, "env.txt")
	argvPath := filepath.Join(dir, "argv.txt")
	ex := &Exec{
		Bin:  bin,
		Home: home,
		Environ: func() []string {
			return []string{
				"FLOCODE_FAKE_ENV=" + envPath,
				"FLOCODE_FAKE_ARGV=" + argvPath,
			}
		},
	}
	res := ex.Verify(context.Background(), Request{}, nil)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	dump, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dump), "MODEL_API_KEY=set") {
		t.Fatalf("MODEL_API_KEY not injected: %s", dump)
	}
	// The key value must not appear in argv.
	argv, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(argv), testKey) {
		t.Fatalf("key leaked into argv: %s", argv)
	}
	// The child must be pinned to Muse Spark 1.2.
	if !strings.Contains(string(argv), MusePin) {
		t.Fatalf("argv missing muse pin: %s", argv)
	}
}

// TestIsVerifyError checks the helper.
func TestIsVerifyError(t *testing.T) {
	if !IsVerifyError(ErrModelAPIKeyMissing) {
		t.Fatal("IsVerifyError(ErrModelAPIKeyMissing) = false")
	}
	if !IsVerifyError(errors.Join(ErrModelAPIKeyMissing, errors.New("x"))) {
		t.Fatal("IsVerifyError(joined) = false")
	}
	if IsVerifyError(errors.New("other")) {
		t.Fatal("IsVerifyError(other) = true")
	}
}
