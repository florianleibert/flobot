package amesh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouteIDRoundTrip(t *testing.T) {
	if got := RouteID("glm-5.2-fp8"); got != "mesh/glm-5.2-fp8" {
		t.Fatalf("RouteID = %q", got)
	}
	if got := ModelIDFromRoute("mesh/glm-5.2-fp8"); got != "glm-5.2-fp8" {
		t.Fatalf("ModelIDFromRoute = %q", got)
	}
	if ModelIDFromRoute("glm") != "" {
		t.Fatal("non-mesh route must return empty model id")
	}
	if !IsMeshRoute("mesh/x") || IsMeshRoute("glm") {
		t.Fatal("IsMeshRoute misclassifies")
	}
}

func TestModelsParsesCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"schema": "amesh.mesh_model_catalog.v1",
			"hub": "https://10.30.0.8:18443",
			"models": [
				{"id": "glm-5.2-fp8", "display": "GLM 5.2", "base_url": "https://10.30.0.8:18443/v1", "health": "ready", "where": "mesh", "importable": true},
				{"id": "qwen3.8-27b-fp8", "health": "ok", "status_text": "ready", "where": "mesh", "importable": true, "context_tokens": 131072},
				{"id": "muse-spark-1.2", "health": "down", "where": "mesh", "importable": false, "import_blocked_reason": "model is unavailable"}
			]
		}`))
	}))
	defer srv.Close()

	c := &Client{Addr: srv.Listener.Addr().String()}
	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("got %d models, want 3", len(models))
	}
	// Both catalog forms are ready: legacy health "ready" and the live
	// catalog's health "ok" + status_text "ready".
	ready := Ready(models)
	if len(ready) != 2 {
		t.Fatalf("Ready = %+v", ready)
	}
	if ready[0].ID != "glm-5.2-fp8" || ready[1].ID != "qwen3.8-27b-fp8" {
		t.Fatalf("Ready order = %+v", ready)
	}
	if _, ok := Find(models, "muse-spark-1.2"); !ok {
		t.Fatal("Find failed for present model")
	}
}

func TestModelsSurfacesUnreachableHub(t *testing.T) {
	c := &Client{Addr: "127.0.0.1:1"}
	if _, err := c.Models(context.Background()); err == nil {
		t.Fatal("expected error for unreachable amesh-web")
	}
}

func TestModelsRejectsWrongSchema(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"schema": "something.else.v1", "models": []}`))
	}))
	defer srv.Close()
	c := &Client{Addr: srv.Listener.Addr().String()}
	if _, err := c.Models(context.Background()); err == nil {
		t.Fatal("expected schema error")
	}
}

func TestConfigTOML(t *testing.T) {
	got := ConfigTOML(Model{ID: "glm-5.2-fp8", BaseURL: "https://10.30.0.8:18443/v1"})
	want := `# flobot generated: amesh mesh model pin. No secrets.
# route mesh/glm-5.2-fp8
default_model = "glm-5.2-fp8"

[models.glm-5.2-fp8]
provider = "openai-compat"
model = "glm-5.2-fp8"
base_url = "https://10.30.0.8:18443/v1"
`
	if got != want {
		t.Fatalf("ConfigTOML:\n%s\nwant:\n%s", got, want)
	}
}

func TestPinNameSanitizes(t *testing.T) {
	if got := PinName("qwen 3.8/27b"); got != "qwen-3.8-27b" {
		t.Fatalf("PinName = %q", got)
	}
	if got := PinName(""); got != "mesh-model" {
		t.Fatalf("PinName(empty) = %q", got)
	}
}

func TestModelsCachesWithinTTL(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Write([]byte(`{"schema":"amesh.mesh_model_catalog.v1","models":[]}`))
	}))
	defer srv.Close()

	// Default-address client path uses the process cache; simulate by
	// pointing the cache at the test server through the env override.
	t.Setenv("AMESH_WEB_ADDR", srv.Listener.Addr().String())
	c := &Client{}
	for i := 0; i < 3; i++ {
		if _, err := c.Models(context.Background()); err != nil {
			t.Fatalf("Models: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("catalog fetched %d times, want 1 (cached)", calls)
	}
}
