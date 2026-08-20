package ccpm

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

var lineRe = regexp.MustCompile(`^\S+ \S+ [A-E] \S+ \S+$`)

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan log: %v", err)
	}
	return lines
}

func countID(lines []string, id string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, " "+id+" ") {
			n++
		}
	}
	return n
}

// TestLogFormat checks the exact line format and that the file is
// created at the requested path.
func TestLogFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "ccpm.log")
	lg := New(path)

	ok, err := lg.Log("pipe-1", "B", "run-42", "ok")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if !ok {
		t.Fatal("Log returned false for a new child_run_id")
	}

	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d: %q", len(lines), lines)
	}
	fields := strings.Fields(lines[0])
	if len(fields) != 5 {
		t.Fatalf("want 5 fields, got %d: %q", len(fields), lines[0])
	}
	if fields[1] != "pipe-1" || fields[2] != "B" || fields[3] != "run-42" || fields[4] != "ok" {
		t.Fatalf("unexpected fields: %q", lines[0])
	}
	if !lineRe.MatchString(lines[0]) {
		t.Fatalf("line does not match format: %q", lines[0])
	}
}

// TestLogDefaultPath checks the default location is ./logs/ccpm.log.
func TestLogDefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	lg := New("")
	if lg.Path() != "logs/ccpm.log" {
		t.Fatalf("Path() = %q, want %q", lg.Path(), "logs/ccpm.log")
	}
	if _, err := lg.Log("p", "A", "r1", "ok"); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "logs", "ccpm.log")); err != nil {
		t.Fatalf("log not created at default path: %v", err)
	}
}

// TestLogNoDuplicates checks that a child_run_id is logged exactly once,
// even when Log is called repeatedly.
func TestLogNoDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ccpm.log")
	lg := New(path)

	for i := 0; i < 5; i++ {
		ok, err := lg.Log("p", "C", "dup-1", "ok")
		if err != nil {
			t.Fatalf("Log %d: %v", i, err)
		}
		if i == 0 && !ok {
			t.Fatal("first Log should append")
		}
		if i > 0 && ok {
			t.Fatalf("Log %d should be a no-op for duplicate id", i)
		}
	}

	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 line, got %d", len(lines))
	}
	if n := countID(lines, "dup-1"); n != 1 {
		t.Fatalf("want 1 line for dup-1, got %d", n)
	}
}

// TestLogNoDuplicatesAcrossRestart checks dedup survives a new Logger
// instance (i.e. a process restart) by scanning the existing file.
func TestLogNoDuplicatesAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ccpm.log")

	lg1 := New(path)
	if _, err := lg1.Log("p", "D", "run-x", "ok"); err != nil {
		t.Fatalf("Log: %v", err)
	}

	lg2 := New(path)
	ok, err := lg2.Log("p", "D", "run-x", "ok")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if ok {
		t.Fatal("duplicate id after restart should not append")
	}

	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d", len(lines))
	}

	// A different id still appends.
	if ok, _ := lg2.Log("p", "E", "run-y", "ok"); !ok {
		t.Fatal("new id should append")
	}
	lines = readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
}

// TestLogConcurrent checks that many concurrent Log calls with distinct
// ids produce exactly one well-formed line each, with no interleaving.
func TestLogConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ccpm.log")
	lg := New(path)

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "run-" + strings.Repeat("x", i%7+1) + string(rune('a'+i%26))
			if _, err := lg.Log("p", "A", id, "ok"); err != nil {
				t.Errorf("Log: %v", err)
			}
		}(i)
	}
	wg.Wait()

	lines := readLines(t, path)
	if len(lines) != n {
		t.Fatalf("want %d lines, got %d", n, len(lines))
	}
	for _, l := range lines {
		if !lineRe.MatchString(l) {
			t.Fatalf("malformed line: %q", l)
		}
	}
}

// TestLogInvalidStage checks that stages outside A-E are rejected.
func TestLogInvalidStage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ccpm.log")
	lg := New(path)

	for _, stage := range []string{"", "F", "a1", "AB"} {
		if _, err := lg.Log("p", stage, "r", "ok"); err == nil {
			t.Fatalf("stage %q should be rejected", stage)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("no log file should be created for invalid stages, stat err = %v", err)
	}
}

// TestLogEmptyChildID checks an empty child_run_id is rejected.
func TestLogEmptyChildID(t *testing.T) {
	lg := New(filepath.Join(t.TempDir(), "ccpm.log"))
	if _, err := lg.Log("p", "A", "  ", "ok"); err == nil {
		t.Fatal("empty child_run_id should be rejected")
	}
}

// TestLogSanitizesFields checks whitespace in fields cannot break the
// single-line format.
func TestLogSanitizesFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ccpm.log")
	lg := New(path)
	if _, err := lg.Log("pipe 1", "b", "run 42", "ok now"); err != nil {
		t.Fatalf("Log: %v", err)
	}
	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d", len(lines))
	}
	// Whitespace is collapsed, so the line stays single-line and the
	// values are preserved verbatim.
	want := "pipe 1 B run 42 ok now"
	if !strings.HasSuffix(lines[0], " "+want) {
		t.Fatalf("line does not end with %q: %q", want, lines[0])
	}
	if strings.ContainsAny(lines[0], "\t\r\n") {
		t.Fatalf("line contains control whitespace: %q", lines[0])
	}
}
