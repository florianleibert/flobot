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

// TestResolvePinDirectRoute proves a direct/<model> route resolves to a pin
// plus a generated config override (base_url + key env), so the headless CLI
// can run direct routes exactly like the server does. Regression for the fix
// that added the direct branch to resolvePin.
func TestResolvePinDirectRoute(t *testing.T) {
	pin, cfg, err := resolvePin(t.Context(), "direct/muse-spark-1.3")
	if err != nil {
		t.Fatal(err)
	}
	if pin != "muse-spark-1.3" {
		t.Fatalf("pin = %q", pin)
	}
	if !strings.Contains(cfg, "api.meta.ai/v1") {
		t.Fatalf("config missing base_url: %s", cfg)
	}
	if !strings.Contains(cfg, `api_key_env = "MODEL_API_KEY"`) {
		t.Fatalf("config missing key env: %s", cfg)
	}
	if strings.Contains(cfg, "sk-") || strings.Contains(cfg, "Bearer") {
		t.Fatalf("config leaked a secret: %s", cfg)
	}

	// Empty direct route is rejected.
	if _, _, err := resolvePin(t.Context(), "direct/"); err == nil {
		t.Fatal("empty direct route should fail")
	}
}
