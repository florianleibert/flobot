package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testKey = "TESTKEY_NOT_FOR_PROD"

func writeKeyFile(t *testing.T, home, content string) string {
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

func TestReadModelAPIKeyTrimsWhitespace(t *testing.T) {
	home := t.TempDir()
	writeKeyFile(t, home, "  "+testKey+"\n\n")
	key, err := ReadModelAPIKey(home)
	if err != nil {
		t.Fatal(err)
	}
	if key != testKey {
		t.Fatalf("key = %q", key)
	}
}

func TestReadModelAPIKeyMissingFile(t *testing.T) {
	home := t.TempDir()
	_, err := ReadModelAPIKey(home)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	// The error must not leak file contents (there are none, but the
	// message must be stable and content-free).
	if strings.Contains(err.Error(), testKey) {
		t.Fatalf("error leaks key: %v", err)
	}
	if !strings.Contains(err.Error(), ".accounts/meta.ai") {
		t.Fatalf("error should name the file: %v", err)
	}
}

func TestReadModelAPIKeyEmptyFile(t *testing.T) {
	home := t.TempDir()
	writeKeyFile(t, home, "   \n\t\n")
	_, err := ReadModelAPIKey(home)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatalf("error leaks key: %v", err)
	}
}

func TestReadModelAPIKeyUnreadableFileNoLeak(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	home := t.TempDir()
	path := writeKeyFile(t, home, testKey)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o600) })
	_, err := ReadModelAPIKey(home)
	if err == nil {
		t.Fatal("expected error for unreadable file")
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatalf("error leaks key: %v", err)
	}
}

func TestWithModelAPIKeySetsAndReplaces(t *testing.T) {
	env := []string{"PATH=/bin", "MODEL_API_KEY=old"}
	out := WithModelAPIKey(env, testKey)
	if len(out) != 2 {
		t.Fatalf("len = %d: %v", len(out), out)
	}
	if out[1] != "MODEL_API_KEY="+testKey {
		t.Fatalf("replace = %v", out)
	}
	// Input slice must not be mutated.
	if env[1] != "MODEL_API_KEY=old" {
		t.Fatalf("input mutated: %v", env)
	}

	env2 := []string{"PATH=/bin"}
	out2 := WithModelAPIKey(env2, testKey)
	if len(out2) != 2 || out2[1] != "MODEL_API_KEY="+testKey {
		t.Fatalf("append = %v", out2)
	}
	if len(env2) != 1 {
		t.Fatalf("input mutated: %v", env2)
	}

	out3 := WithModelAPIKey(nil, testKey)
	if len(out3) != 1 || out3[0] != "MODEL_API_KEY="+testKey {
		t.Fatalf("nil env = %v", out3)
	}
}

// TestChildEnvExposesKeyOnlyAsEnvVar runs a fake child and asserts the key
// reaches the child only through the MODEL_API_KEY env var: it is set in
// the child environment, but the fake's env dump (which only records
// presence, never values) and the NDJSON stream contain no key material.
func TestChildEnvExposesKeyOnlyAsEnvVar(t *testing.T) {
	dir := t.TempDir()
	bin, err := WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	writeKeyFile(t, home, testKey+"\n")
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
	dump, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dump), "MODEL_API_KEY=set") {
		t.Fatalf("key not in child env: %s", dump)
	}
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
