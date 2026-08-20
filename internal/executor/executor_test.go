package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFindBinaryEnv(t *testing.T) {
	t.Setenv("FLOCODE_BIN", "/tmp/fake-flocode-bin")
	p, err := FindBinary()
	if err != nil || p != "/tmp/fake-flocode-bin" {
		t.Fatalf("%q %v", p, err)
	}
}

func TestArgvContract(t *testing.T) {
	args := Argv(Request{
		Prompt:        "hello",
		Pin:           "glm-5.2",
		Intent:        "ask",
		Yolo:          true,
		Verify:        true,
		Worktree:      "wt1",
		ContinueUntil: true,
	})
	want := []string{
		"-p", "hello",
		"--model", "glm-5.2",
		"--intent", "ask",
		"--output", "streaming-json",
		"--yolo",
		"--verify",
		"--worktree", "wt1",
		"--continue-until",
	}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %#v", args)
	}
	plain := Argv(Request{Prompt: "x", Pin: "qwen-3.8"})
	joined := strings.Join(plain, " ")
	if strings.Contains(joined, "--yolo") || strings.Contains(joined, "--verify") {
		t.Fatalf("optional flags leaked: %v", plain)
	}
	if plain[5] != "code" {
		t.Fatalf("default intent: %v", plain)
	}
}

func TestRunFakeBinaryTwoNDJSONLines(t *testing.T) {
	dir := t.TempDir()
	bin, err := WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	argvPath := filepath.Join(dir, "argv.txt")
	ex := &Exec{
		Bin: bin,
		Environ: func() []string {
			return []string{"FLOCODE_FAKE_ARGV=" + argvPath}
		},
	}
	var seen []Event
	res := ex.Run(context.Background(), Request{
		Prompt: "hello",
		Pin:    "glm-5.2",
		Intent: "code",
		Route:  "glm",
	}, func(ev Event) { seen = append(seen, ev) })
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if len(res.Events) != 2 {
		t.Fatalf("events = %#v", res.Events)
	}
	if res.Events[0].Kind != "content" || res.Events[0].Text != "hello from fake" || res.Events[0].TurnID != 1 {
		t.Fatalf("line0 %+v", res.Events[0])
	}
	if res.Events[1].Kind != "done" {
		t.Fatalf("line1 %+v", res.Events[1])
	}
	if len(seen) != 2 {
		t.Fatalf("emit count %d", len(seen))
	}
	raw, err := json.Marshal(res.Events)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-tool-arg") || strings.Contains(string(raw), `"args"`) {
		t.Fatalf("tool args echoed: %s", raw)
	}
	b, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, need := range []string{"-p", "hello", "--model", "glm-5.2", "--intent", "code", "--output", "streaming-json"} {
		if !strings.Contains(got, need) {
			t.Fatalf("argv missing %q: %s", need, got)
		}
	}
	if strings.Contains(got, "--yolo") {
		t.Fatalf("yolo without request: %s", got)
	}
}

func TestMuseKeyInjectedWithoutLogging(t *testing.T) {
	dir := t.TempDir()
	bin, err := WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".accounts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".accounts", "meta.ai"), []byte("TESTKEY_NOT_FOR_PROD\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(dir, "env.txt")
	ex := &Exec{
		Bin:  bin,
		Home: home,
		Environ: func() []string {
			return []string{"FLOCODE_FAKE_ENV=" + envPath}
		},
	}
	res := ex.Run(context.Background(), Request{Prompt: "p", Pin: "muse-spark-1.2", Intent: "ask", Route: "muse"}, nil)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	got, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "MODEL_API_KEY=set") {
		t.Fatalf("muse key not injected: %s", got)
	}
	if strings.Contains(string(got), "TESTKEY_NOT_FOR_PROD") {
		t.Fatal("key value written to fake env dump")
	}
}

func TestVerifyInjectsMuseKeyOnGlmRoute(t *testing.T) {
	dir := t.TempDir()
	bin, err := WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".accounts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".accounts", "meta.ai"), []byte("TESTKEY_NOT_FOR_PROD\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(dir, "env-verify.txt")
	ex := &Exec{
		Bin:  bin,
		Home: home,
		Environ: func() []string {
			return []string{"FLOCODE_FAKE_ENV=" + envPath}
		},
	}
	res := ex.Run(context.Background(), Request{Prompt: "p", Pin: "glm-5.2", Intent: "code", Route: "glm", Verify: true}, nil)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	got, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "MODEL_API_KEY=set") {
		t.Fatalf("verify key not injected on glm: %s", got)
	}
}

func TestParseDropsUnknownFields(t *testing.T) {
	ev, ok := parseLine([]byte(`{"kind":"tool_end","turn_id":2,"tool":"bash","is_error":true,"args":{"command":"cat /secrets"},"result":"nope"}`))
	if !ok {
		t.Fatal("parse")
	}
	if ev.Kind != "tool_end" || ev.Tool != "bash" || ev.TurnID != 2 || ev.IsError == nil || !*ev.IsError {
		t.Fatalf("%+v", ev)
	}
	raw, _ := json.Marshal(ev)
	if strings.Contains(string(raw), "cat /secrets") || strings.Contains(string(raw), "args") {
		t.Fatalf("unknown fields kept: %s", raw)
	}
}

func TestCancelKillsProcess(t *testing.T) {
	dir := t.TempDir()
	bin, err := WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	ex := &Exec{
		Bin: bin,
		Environ: func() []string {
			return []string{"FLOCODE_FAKE_SLEEP=5"}
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	res := ex.Run(ctx, Request{Prompt: "p", Pin: "glm-5.2", Timeout: time.Second}, nil)
	if time.Since(start) > 2*time.Second {
		t.Fatalf("cancel too slow: %s", time.Since(start))
	}
	if res.Err == nil {
		t.Fatal("expected cancel error")
	}
}
