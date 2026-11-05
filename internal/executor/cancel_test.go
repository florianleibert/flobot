package executor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// M4 acceptance: a cancelled routine leaves nothing half-done.
//
// SIGKILL alone gives the child no chance to flush a partial result, remove a
// temp file or release a lock. The child here traps SIGTERM and writes a marker,
// which only happens if the cancel path asks before it kills.
func TestCancelAsksBeforeItKills(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "cleaned-up")
	bin := filepath.Join(dir, "flocode")
	script := "#!/bin/sh\n" +
		"trap 'printf done > " + marker + "; exit 0' TERM\n" +
		"printf '%s\\n' '{\"kind\":\"content\",\"turn_id\":1,\"text\":\"working\"}'\n" +
		// Wait for the signal rather than exiting on its own.
		"while :; do sleep 0.05; done\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ex := &Exec{Bin: bin, Environ: func() []string { return []string{"PATH=" + os.Getenv("PATH")} }}

	started := make(chan struct{})
	var once bool
	go func() {
		// Wait for the child's first event, not for a fixed delay: the event is
		// printed after the trap is installed, so this is what proves SIGTERM
		// will be handled. A sleep here races process startup — under load the
		// signal can arrive before the trap exists and the child dies by
		// default action, which looks like a cancel bug but is a test bug.
		<-started
		cancel()
	}()

	res := ex.Run(ctx, Request{Pin: "glm", Intent: "ask"}, func(ev Event) {
		if !once {
			once = true
			close(started)
		}
	})
	if res.Err == nil {
		t.Fatal("a cancelled run reported no error")
	}
	select {
	case <-started:
	default:
		t.Fatal("the child emitted no event before the cancel")
	}

	// The trap must have run: SIGTERM was delivered before SIGKILL.
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the child was killed without a chance to clean up: %v", err)
	}
}

// A cancelled run must not outlive its cancel by much: the grace is bounded, so
// an unresponsive child is killed rather than left running.
func TestCancelKillsAnUnresponsiveChild(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the cancel grace")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "flocode")
	// Ignores SIGTERM entirely: only SIGKILL stops it.
	script := "#!/bin/sh\n" +
		"trap '' TERM\n" +
		"printf '%s\\n' '{\"kind\":\"content\",\"turn_id\":1,\"text\":\"stubborn\"}'\n" +
		"while :; do sleep 0.05; done\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ex := &Exec{Bin: bin, Environ: func() []string { return []string{"PATH=" + os.Getenv("PATH")} }}
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	res := ex.Run(ctx, Request{Pin: "glm", Intent: "ask"}, nil)
	if res.Err == nil {
		t.Fatal("a cancelled run reported no error")
	}
	if elapsed := time.Since(start); elapsed > CancelGrace+5*time.Second {
		t.Fatalf("cancel took %v: the grace is not bounded", elapsed)
	}
}
