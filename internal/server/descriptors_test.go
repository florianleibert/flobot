package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// descriptorWorkspace spins up a server whose workspace holds one file.
func descriptorWorkspace(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	if p, err := filepath.EvalSymlinks(dir); err == nil {
		dir = p
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := New(nil, WithHome(t.TempDir()), WithWorkDir(dir))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, dir
}

// putJSON sends a PUT with a JSON body.
func putJSON(t *testing.T, ts *httptest.Server, path string, body map[string]any) (int, string) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPut, ts.URL+path, strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestDescriptorRoutesOpenReadWrite(t *testing.T) {
	ts, dir := descriptorWorkspace(t)

	// Open returns 201 and a descriptor with an id.
	res := postJSONStatus(t, ts, "/api/v1/workspace/descriptors", map[string]any{"path": "main.go"})
	if res.code != 201 {
		t.Fatalf("open: status %d, want 201 (%s)", res.code, res.body)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(res.body), &d); err != nil {
		t.Fatal(err)
	}
	id, ok := d["id"].(float64)
	if !ok || id <= 0 {
		t.Fatalf("descriptor id = %v, want a positive number", d["id"])
	}
	if d["path"] != "main.go" {
		t.Errorf("path = %v, want main.go", d["path"])
	}

	// The descriptor list shows it.
	list := getJSON(t, ts, "/api/v1/workspace/descriptors")
	descs, _ := list["descriptors"].([]any)
	if len(descs) != 1 {
		t.Fatalf("descriptors = %v, want 1 entry", list["descriptors"])
	}

	// Read through the descriptor returns the content.
	got := getJSON(t, ts, "/api/v1/workspace/descriptors/1")
	if got["content"] != "package main\n" {
		t.Errorf("content = %v", got["content"])
	}

	// Write through the descriptor lands on disk.
	code, body := putJSON(t, ts, "/api/v1/workspace/descriptors/1", map[string]any{
		"content": "package main\n\nfunc main() {}\n",
	})
	if code != 200 {
		t.Fatalf("write: status %d (%s)", code, body)
	}
	onDisk, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != "package main\n\nfunc main() {}\n" {
		t.Errorf("on disk = %q", onDisk)
	}

	// Close it; reading afterwards is a 404.
	code, body = putJSON(t, ts, "/api/v1/workspace/descriptors/1", map[string]any{"content": "x"})
	if code != 200 {
		t.Fatalf("second write: status %d (%s)", code, body)
	}
	resp, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/workspace/descriptors/1", nil)
	if err != nil {
		t.Fatal(err)
	}
	delResp, err := http.DefaultClient.Do(resp)
	if err != nil {
		t.Fatal(err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != 200 {
		t.Fatalf("close: status %d", delResp.StatusCode)
	}
	resp2, err := http.Get(ts.URL + "/api/v1/workspace/descriptors/1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 404 {
		t.Errorf("read after close: status %d, want 404", resp2.StatusCode)
	}
}

func TestDescriptorRouteRejectsEscape(t *testing.T) {
	ts, _ := descriptorWorkspace(t)
	res := postJSONStatus(t, ts, "/api/v1/workspace/descriptors", map[string]any{"path": "../escape.go"})
	if res.code != 400 {
		t.Fatalf("escape open: status %d, want 400 (%s)", res.code, res.body)
	}
	res = postJSONStatus(t, ts, "/api/v1/workspace/descriptors", map[string]any{"path": ".git/config"})
	if res.code != 400 {
		t.Fatalf("protected open: status %d, want 400 (%s)", res.code, res.body)
	}
}

func TestDescriptorRouteInvalidID(t *testing.T) {
	ts, _ := descriptorWorkspace(t)
	resp, err := http.Get(ts.URL + "/api/v1/workspace/descriptors/abc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("invalid id: status %d, want 400", resp.StatusCode)
	}
}

func TestDescriptorRouteConflictOnExternalChange(t *testing.T) {
	ts, dir := descriptorWorkspace(t)
	res := postJSONStatus(t, ts, "/api/v1/workspace/descriptors", map[string]any{"path": "main.go"})
	if res.code != 201 {
		t.Fatalf("open: status %d (%s)", res.code, res.body)
	}
	// An external writer (a flocode run) touches the file.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("// external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, body := putJSON(t, ts, "/api/v1/workspace/descriptors/1", map[string]any{"content": "mine\n"})
	if code != 409 {
		t.Fatalf("conflicting write: status %d, want 409 (%s)", code, body)
	}
	onDisk, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if string(onDisk) != "// external\n" {
		t.Errorf("external change lost: %q", onDisk)
	}
}
