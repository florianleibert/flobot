package amesh

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeFakeAmesh writes a tiny shell script that answers `hubs list` and
// `models list` with the given JSON, and returns its path. The amesh package
// resolves the binary via AMESH_BIN (getenv), so tests point it here.
func writeFakeAmesh(t *testing.T, hubsJSON, modelsJSON string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "amesh")
	var script string
	if runtime.GOOS == "windows" {
		script = "@echo off\r\n"
	} else {
		script = "#!/bin/sh\n"
	}
	// The CLI is invoked as `amesh <cmd> <sub> --json`. Branch on the first
	// two args so one fake binary serves both discovery lists.
	script += `case "$1 $2" in
  "hubs list") echo '` + hubsJSON + `';;
  "models list") echo '` + modelsJSON + `';;
  *) echo '{}';;
esac
`
	if runtime.GOOS == "windows" {
		if err := os.WriteFile(bin, []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AMESH_BIN", bin)
	return bin
}

func TestHubsParsesJSON(t *testing.T) {
	resetCaches()
	writeFakeAmesh(t,
		`[{"name":"hub-a","url":"https://hub-a:8440","controller_url":"http://10.0.0.1:18787","fingerprint":"sha256:abc","default":true,"wg_up":true,"wg_status":{"up":true,"ip":"10.0.0.5","real_iface":"utun1"}}]`,
		`{"schema":"amesh.accessible_models.v1","models":[{"id":"service-1","display":"M1","location":"local","state":"ready","profile":"m1","context_tokens":8192}]}`,
	)
	hubs, err := Hubs(context.Background())
	if err != nil {
		t.Fatalf("Hubs: %v", err)
	}
	if len(hubs) != 1 {
		t.Fatalf("got %d hubs, want 1", len(hubs))
	}
	h := hubs[0]
	if h.Name != "hub-a" || !h.Default || !h.WGUp {
		t.Fatalf("hub = %+v", h)
	}
	if h.WGStatus == nil || h.WGStatus.IP != "10.0.0.5" {
		t.Fatalf("wg status = %+v", h.WGStatus)
	}
}

func TestServicesParsesJSON(t *testing.T) {
	resetCaches()
	writeFakeAmesh(t,
		`[]`,
		`{"schema":"amesh.accessible_models.v1","models":[
			{"id":"service-1","display":"Local M","location":"local","state":"ready","profile":"m1","context_tokens":8192},
			{"id":"service-2","display":"Hub M","location":"51.81.184.130","state":"stopped"}
		]}`,
	)
	svcs, err := Services(context.Background())
	if err != nil {
		t.Fatalf("Services: %v", err)
	}
	if len(svcs) != 2 {
		t.Fatalf("got %d services, want 2", len(svcs))
	}
	if svcs[0].ID != "service-1" || svcs[0].State != "ready" || svcs[0].Location != "local" {
		t.Fatalf("svc[0] = %+v", svcs[0])
	}
	if svcs[1].Location != "51.81.184.130" || svcs[1].State != "stopped" {
		t.Fatalf("svc[1] = %+v", svcs[1])
	}
	ready := ReadyServices(svcs)
	if len(ready) != 1 || ready[0].ID != "service-1" {
		t.Fatalf("ReadyServices = %+v", ready)
	}
}

func TestServicesEmptyIsValid(t *testing.T) {
	resetCaches()
	writeFakeAmesh(t, `[]`, `{"schema":"amesh.accessible_models.v1","models":[]}`)
	svcs, err := Services(context.Background())
	if err != nil {
		t.Fatalf("Services: %v", err)
	}
	if len(svcs) != 0 {
		t.Fatalf("got %d services, want 0", len(svcs))
	}
}

func TestHubsMissingBinary(t *testing.T) {
	resetCaches()
	t.Setenv("AMESH_BIN", filepath.Join(t.TempDir(), "does-not-exist"))
	if _, err := Hubs(context.Background()); err == nil {
		t.Fatal("expected error for missing amesh binary")
	}
}
