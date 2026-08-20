package main

import (
	"strings"
	"testing"

	"github.com/florianleibert/flobot/internal/executor"
)

func TestServeRejectsNonLoopback(t *testing.T) {
	err := run([]string{"flobot", "serve", "--addr", "0.0.0.0:4120"})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("got %v", err)
	}
	err = run([]string{"flobot", "serve", "--addr", ":4120"})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("got %v", err)
	}
}

func TestRunCLIFakeBinary(t *testing.T) {
	bin, err := executor.WriteFakeBinary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLOCODE_BIN", bin)
	if err := run([]string{"flobot", "run", "--prompt", "hi", "--route", "glm"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"flobot", "run", "--prompt", "hi", "--route", "local"}); err == nil {
		t.Fatal("local route should fail")
	}
}

func TestRunRequiresPrompt(t *testing.T) {
	err := run([]string{"flobot", "run", "--route", "glm"})
	if err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Fatalf("got %v", err)
	}
}
