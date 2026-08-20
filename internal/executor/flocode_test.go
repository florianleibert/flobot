package executor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// writeFlocodeFake writes a shell script that echoes its argv to a file
// (when FLOCODE_FAKE_ARGV is set) and prints a few NDJSON lines, including
// one non-JSON line that must be skipped.
func writeFlocodeFake(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix shell fake")
	}
	path := filepath.Join(dir, "flocode")
	script := `#!/bin/sh
if [ -n "$FLOCODE_FAKE_ARGV" ]; then
  printf '%s\n' "$0" "$@" > "$FLOCODE_FAKE_ARGV"
fi
printf '%s\n' '{"kind":"content","turn_id":1,"text":"hi"}'
printf 'not json at all\n'
printf '%s\n' '{"kind":"done","turn_id":1}'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveFlocodeViaPATH(t *testing.T) {
	dir := t.TempDir()
	writeFlocodeFake(t, dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	p, err := ResolveFlocode()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "flocode" {
		t.Fatalf("resolved %q", p)
	}
}

func TestResolveFlocodeMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := ResolveFlocode(); err == nil {
		t.Fatal("expected error when flocode not in PATH")
	}
}

func TestFlocodeRunSpawnsAndForwardsMaps(t *testing.T) {
	dir := t.TempDir()
	bin := writeFlocodeFake(t, dir)
	argvPath := filepath.Join(dir, "argv.txt")

	f := &Flocode{
		Bin: bin,
		Environ: func() []string {
			return []string{"FLOCODE_FAKE_ARGV=" + argvPath}
		},
	}

	out := make(chan map[string]any, 8)
	errCh := make(chan error, 1)
	go func() { errCh <- f.Run(context.Background(), "hello world", out) }()

	var got []map[string]any
	for m := range out {
		got = append(got, m)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 {
		t.Fatalf("maps = %#v (want 2, non-JSON line skipped)", got)
	}
	if got[0]["kind"] != "content" || got[0]["text"] != "hi" {
		t.Fatalf("map0 = %#v", got[0])
	}
	if got[1]["kind"] != "done" {
		t.Fatalf("map1 = %#v", got[1])
	}

	// Verify the child was spawned with -p <prompt> --output streaming-json.
	b, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	argv := string(b)
	for _, want := range []string{"-p", "hello world", "--output", "streaming-json"} {
		if !containsLine(argv, want) {
			t.Fatalf("argv missing %q: %s", want, argv)
		}
	}
}

func TestFlocodeRunMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	f := &Flocode{}
	out := make(chan map[string]any, 1)
	err := f.Run(context.Background(), "p", out)
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestFlocodeRunContextCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell fake")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "flocode")
	script := "#!/bin/sh\nsleep 5\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	f := &Flocode{Bin: path}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	out := make(chan map[string]any, 1)
	start := time.Now()
	_ = f.Run(ctx, "p", out)
	if time.Since(start) > 2*time.Second {
		t.Fatalf("cancel too slow: %s", time.Since(start))
	}
}

func TestParseNDJSONLine(t *testing.T) {
	m, ok := parseNDJSONLine([]byte(`  {"a":1,"b":"x"}  `))
	if !ok || m["a"] != float64(1) || m["b"] != "x" {
		t.Fatalf("got %#v ok=%v", m, ok)
	}
	if _, ok := parseNDJSONLine([]byte("")); ok {
		t.Fatal("blank line should be skipped")
	}
	if _, ok := parseNDJSONLine([]byte("not json")); ok {
		t.Fatal("non-JSON should be skipped")
	}
	if _, ok := parseNDJSONLine([]byte(`[1,2]`)); ok {
		t.Fatal("non-object JSON should be skipped")
	}
}

func containsLine(s, sub string) bool {
	for _, line := range splitLines(s) {
		if line == sub {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	out = append(out, cur)
	return out
}
