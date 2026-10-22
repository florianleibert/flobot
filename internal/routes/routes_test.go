package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestDefaultRoutesDocumentTheCombination(t *testing.T) {
	got := map[string]Route{}
	for _, r := range DefaultRoutes() {
		got[r.ID] = r
	}
	for _, id := range []string{"glm", "qwen", "local", "muse"} {
		if _, ok := got[id]; !ok {
			t.Fatalf("missing route %s", id)
		}
	}
	if got["glm"].Model != "glm-5.2-fp8" || got["muse"].Model != "muse-spark-1.3" || got["qwen"].Model != "qwen3.8-27b-fp8" {
		t.Fatalf("%+v", got)
	}
	if got["muse"].Source != "meta-model-api" {
		t.Fatalf("muse source = %s", got["muse"].Source)
	}
}

func TestPinMapAndResolvePin(t *testing.T) {
	if PinMap["glm"] != "glm-5.2" || PinMap["qwen"] != "qwen-3.8" || PinMap["muse"] != "muse-spark-1.3" {
		t.Fatalf("PinMap = %#v", PinMap)
	}
	if _, ok := PinMap["local"]; ok {
		t.Fatal("local must not have a pin until one exists")
	}
	pin, err := ResolvePin("glm")
	if err != nil || pin != "glm-5.2" {
		t.Fatalf("glm pin = %q %v", pin, err)
	}
	pin, err = ResolvePin("qwen")
	if err != nil || pin != "qwen-3.8" {
		t.Fatalf("qwen pin = %q %v", pin, err)
	}
	pin, err = ResolvePin("muse")
	if err != nil || pin != "muse-spark-1.3" {
		t.Fatalf("muse pin = %q %v", pin, err)
	}
	if _, err := ResolvePin("local"); !errors.Is(err, ErrLocalUnpinned) {
		t.Fatalf("local: %v", err)
	}
	if _, err := ResolvePin("nope"); err == nil {
		t.Fatal("unknown route must fail")
	}
	pin, err = ResolvePin("")
	if err != nil || pin != "glm-5.2" {
		t.Fatalf("empty route should default glm: %q %v", pin, err)
	}
}

func TestRouteJSONHasNoSecretsOrURLs(t *testing.T) {
	raw, err := json.Marshal(DefaultRoutes())
	if err != nil {
		t.Fatal(err)
	}
	low := strings.ToLower(string(raw))
	for _, leak := range []string{
		"http://", "https://", "api_key", "token", "password", "bearer",
		"10.30.0.127", "23.183.40.72", "10.10.0.14", "api.meta.ai",
		"model_api_key", "/v1",
	} {
		if strings.Contains(low, leak) {
			t.Fatalf("route JSON leaked %q: %s", leak, raw)
		}
	}
}

func TestLocalPinLifecycle(t *testing.T) {
	// Unpinned by default: shape-valid, but not resolvable.
	if err := Validate("local"); err != nil {
		t.Fatalf("local shape should always validate: %v", err)
	}
	if _, err := ResolvePin("local"); !errors.Is(err, ErrLocalUnpinned) {
		t.Fatalf("unpinned local resolve: %v", err)
	}
	// Pin it.
	SetLocalPin("qwen3-coder-30b-a3b-mlx")
	defer SetLocalPin("")
	if err := Validate("local"); err != nil {
		t.Fatalf("pinned local validate: %v", err)
	}
	pin, err := ResolvePin("local")
	if err != nil || pin != "qwen3-coder-30b-a3b-mlx" {
		t.Fatalf("pinned local resolve = %q %v", pin, err)
	}
	// Unpin.
	SetLocalPin("")
	if err := Validate("local"); err != nil {
		t.Fatalf("re-unpinned local still shape-valid: %v", err)
	}
	if _, err := ResolvePin("local"); !errors.Is(err, ErrLocalUnpinned) {
		t.Fatalf("re-unpinned local resolve: %v", err)
	}
}

func TestValidateRouteShapes(t *testing.T) {
	cases := []struct {
		id   string
		want error
	}{
		{"", nil},             // defaults to glm
		{"glm", nil},          // documented
		{"qwen", nil},         // documented
		{"muse", nil},         // documented
		{"local", nil},        // shape-valid even unpinned
		{"mesh/glm-5.2-fp8", nil}, // shape-valid; readiness at spawn
		{"mesh/", fmt.Errorf("empty mesh route")},
		{"direct/muse-spark-1.3", nil},
		{"direct/", fmt.Errorf("empty direct route")},
		{"nope", fmt.Errorf("unknown route")},
	}
	for _, c := range cases {
		err := Validate(c.id)
		if c.want == nil && err != nil {
			t.Fatalf("Validate(%q) = %v, want nil", c.id, err)
		}
		if c.want != nil && err == nil {
			t.Fatalf("Validate(%q) = nil, want error", c.id)
		}
		if c.want != nil && !strings.Contains(err.Error(), "unknown route") && c.id == "nope" {
			t.Fatalf("Validate(%q) = %v, want unknown-route error", c.id, err)
		}
	}
}
