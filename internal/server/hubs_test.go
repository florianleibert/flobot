package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/florianleibert/flobot/internal/store"
)

// writeFakeAmesh writes a shell script that answers `hubs list`, `models
// list`, `models chat <id>`, `hubs refresh`, and `doctor` with the given
// JSON / output, and points AMESH_BIN at it. The amesh discovery cache is
// keyed by binary path, so each test's distinct temp binary never collides
// with another.
func writeFakeAmesh(t *testing.T, hubsJSON, modelsJSON string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "amesh")
	var script string
	if runtime.GOOS == "windows" {
		script = "@echo off\r\n"
	} else {
		script = "#!/bin/sh\n"
	}
	script += `case "$1 $2" in
  "hubs list") echo '` + hubsJSON + `';;
  "models list") echo '` + modelsJSON + `';;
  "models chat") echo "pong";;
  "hubs refresh") echo "refreshed 2/2 hubs (0 failed)";;
  "doctor --json") echo '{"ok":true,"root":"/tmp/.amesh","default_hub":"hub-a","checks":[{"id":"wireguard","status":"ok","message":"wg up"},{"id":"hub_config","status":"ok","message":"Hub \"hub-a\" has local URL"}]}';;
  *) echo '{}';;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AMESH_BIN", bin)
}

func TestHubModelsEndpoint(t *testing.T) {
	writeFakeAmesh(t,
		`[{"name":"hub-a","url":"https://hub-a:8440","controller_url":"http://10.0.0.1:18787","default":true,"wg_up":true,"wg_status":{"up":true,"ip":"10.0.0.5"}}]`,
		`{"schema":"amesh.accessible_models.v1","models":[
			{"id":"service-1","display":"Local M","location":"local","state":"ready","profile":"m1"},
			{"id":"service-2","display":"Hub M","location":"hub-a","state":"stopped"}
		]}`,
	)
	srv := New(store.New())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/v1/hubs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body struct {
		OK   bool `json:"ok"`
		Hubs []struct {
			Name    string `json:"name"`
			WGUp    bool   `json:"wg_up"`
			Default bool   `json:"default"`
		} `json:"hubs"`
		Models []struct {
			ID       string `json:"id"`
			Location string `json:"location"`
			State    string `json:"state"`
		} `json:"models"`
	}
	if err := jsonDecode(resp, &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK {
		t.Fatal("ok=false")
	}
	if len(body.Hubs) != 1 || body.Hubs[0].Name != "hub-a" || !body.Hubs[0].WGUp || !body.Hubs[0].Default {
		t.Fatalf("hubs = %+v", body.Hubs)
	}
	if len(body.Models) != 2 || body.Models[0].Location != "local" || body.Models[1].State != "stopped" {
		t.Fatalf("models = %+v", body.Models)
	}
}

func TestHubModelsEndpointMissingAmesh(t *testing.T) {
	t.Setenv("AMESH_BIN", filepath.Join(t.TempDir(), "nope"))
	srv := New(store.New())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/v1/hubs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("missing amesh must render as a state, not a 5xx; got %d", resp.StatusCode)
	}
	var body struct {
		OK bool `json:"ok"`
	}
	if err := jsonDecode(resp, &body); err != nil {
		t.Fatal(err)
	}
	if body.OK {
		t.Fatal("ok must be false when amesh is missing")
	}
}

func TestModelStartEndpoint(t *testing.T) {
	writeFakeAmesh(t, `[]`,
		`{"schema":"amesh.accessible_models.v1","models":[
			{"id":"service-1","display":"Local M","location":"local","state":"stopped","profile":"m1"},
			{"id":"service-2","display":"Hub M","location":"hub-a","state":"stopped"}
		]}`,
	)
	srv := New(store.New())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// Stopped local model -> warm-up accepted.
	resp, err := http.Post(ts.URL+"/api/v1/models/service-1/start", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		OK    bool   `json:"ok"`
		State string `json:"state"`
	}
	if err := jsonDecode(resp, &body); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !body.OK || body.State != "starting" {
		t.Fatalf("start = %+v", body)
	}

	// Stopped hub model -> also warmable (amesh chat verifies the route).
	resp, err = http.Post(ts.URL+"/api/v1/models/service-2/start", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := jsonDecode(resp, &body); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !body.OK || body.State != "starting" {
		t.Fatalf("hub start = %+v", body)
	}

	// Unknown model -> 404.
	resp, err = http.Post(ts.URL+"/api/v1/models/nope/start", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown model must be 404, got %d", resp.StatusCode)
	}
}

func jsonDecode(resp *http.Response, out any) error {
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

func TestHubRefreshEndpoint(t *testing.T) {
	writeFakeAmesh(t,
		`[{"name":"hub-a","url":"https://hub-a:8440","default":true,"wg_up":true}]`,
		`{"schema":"amesh.accessible_models.v1","models":[{"id":"service-1","display":"M","location":"local","state":"ready"}]}`,
	)
	srv := New(store.New())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Post(ts.URL+"/api/v1/hubs/refresh", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		OK      bool   `json:"ok"`
		Summary string `json:"summary"`
		Hubs    []struct {
			Name string `json:"name"`
		} `json:"hubs"`
	}
	if err := jsonDecode(resp, &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK {
		t.Fatal("refresh ok=false")
	}
	if body.Summary != "refreshed 2/2 hubs (0 failed)" {
		t.Fatalf("summary = %q", body.Summary)
	}
	if len(body.Hubs) != 1 || body.Hubs[0].Name != "hub-a" {
		t.Fatalf("hubs = %+v", body.Hubs)
	}
}

func TestHubDoctorEndpoint(t *testing.T) {
	writeFakeAmesh(t, `[]`, `{"schema":"amesh.accessible_models.v1","models":[]}`)
	srv := New(store.New())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/v1/hubs/doctor")
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		OK         bool   `json:"ok"`
		DefaultHub string `json:"default_hub"`
		Checks     []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	if err := jsonDecode(resp, &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK {
		t.Fatal("doctor ok=false")
	}
	if body.DefaultHub != "hub-a" {
		t.Fatalf("default_hub = %q", body.DefaultHub)
	}
	if len(body.Checks) != 2 || body.Checks[0].ID != "wireguard" || body.Checks[0].Status != "ok" {
		t.Fatalf("checks = %+v", body.Checks)
	}
}

func TestHubDetailEndpoint(t *testing.T) {
	writeFakeAmesh(t,
		`[{"name":"hub-a","url":"https://hub-a:8440","default":true,"wg_up":true}]`,
		`{"schema":"amesh.accessible_models.v1","models":[]}`,
	)
	srv := New(store.New())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/v1/hubs/hub-a")
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Hub struct {
			Name    string `json:"name"`
			WGUp    bool   `json:"wg_up"`
			Default bool   `json:"default"`
		} `json:"hub"`
		Checks []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	if err := jsonDecode(resp, &body); err != nil {
		t.Fatal(err)
	}
	if body.Hub.Name != "hub-a" || !body.Hub.WGUp || !body.Hub.Default {
		t.Fatalf("hub = %+v", body.Hub)
	}
	if len(body.Checks) == 0 {
		t.Fatal("expected doctor checks mentioning hub-a")
	}

	// Unknown hub -> 404.
	resp, err = http.Get(ts.URL + "/api/v1/hubs/nope")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown hub must be 404, got %d", resp.StatusCode)
	}
}
