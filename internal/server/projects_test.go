package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/store"
)

// patchJSON sends a PATCH with a JSON body and returns the decoded response.
func patchJSON(t *testing.T, ts *httptest.Server, path string, body map[string]any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPatch, ts.URL+path, strings.NewReader(string(raw)))
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
	if resp.StatusCode >= 300 {
		t.Fatalf("%s -> %d %s", path, resp.StatusCode, b)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%s: %s", path, b)
	}
	return out
}

// TestProjectsAndTags covers the project/tag surface behind the Code view:
// create a project, tag it, filter by tag, and point a run at a project or a
// tag so the agent is told which files it may touch.
func TestProjectsAndTags(t *testing.T) {
	// Isolate the persisted projects file from the real ~/.flobot.
	t.Setenv("FLOBOT_TEST_STATE", t.TempDir())

	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New()
	srv := New(st, WithExec(&executor.Exec{Bin: bin}))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// A project names a folder, a set of files in scope and its tags.
	p := postJSON(t, ts, "/api/v1/projects", map[string]any{
		"name":  "API",
		"root":  "internal",
		"paths": []string{"internal/server"},
		"tags":  []string{"api"},
	})
	id, _ := p["id"].(string)
	if id == "" {
		t.Fatalf("project id missing: %v", p)
	}
	if p["name"] != "API" {
		t.Fatalf("name = %v", p["name"])
	}

	// The list carries the tag vocabulary with it.
	list := getJSON(t, ts, "/api/v1/projects")
	projects, _ := list["projects"].([]any)
	if len(projects) != 1 {
		t.Fatalf("projects = %v", list["projects"])
	}
	tags, _ := list["tags"].([]any)
	if len(tags) != 1 {
		t.Fatalf("tags = %v", list["tags"])
	}

	// A tag can be created on its own, then applied to a project.
	postJSON(t, ts, "/api/v1/tags", map[string]any{"name": "frontend"})
	patched := patchJSON(t, ts, "/api/v1/projects/"+id, map[string]any{
		"tags": []string{"api", "frontend"},
	})
	ptags, _ := patched["tags"].([]any)
	if len(ptags) != 2 {
		t.Fatalf("patched tags = %v", patched["tags"])
	}

	// The tag list counts the projects wearing each label.
	tl := getJSON(t, ts, "/api/v1/tags")
	tlTags, _ := tl["tags"].([]any)
	if len(tlTags) != 2 {
		t.Fatalf("tags = %v", tl["tags"])
	}
	for _, raw := range tlTags {
		tag := raw.(map[string]any)
		if tag["projects"].(float64) != 1 {
			t.Fatalf("tag %v count = %v", tag["name"], tag["projects"])
		}
	}

	// Filtering by tag selects the projects that carry it.
	filtered := getJSON(t, ts, "/api/v1/projects?tag=frontend")
	fps, _ := filtered["projects"].([]any)
	if len(fps) != 1 {
		t.Fatalf("filtered = %v", filtered["projects"])
	}
	none := getJSON(t, ts, "/api/v1/projects?tag=nothing")
	nps, _ := none["projects"].([]any)
	if len(nps) != 0 {
		t.Fatalf("filtered by unknown tag = %v", none["projects"])
	}

	// A run pointed at the project carries the scope with it.
	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{
		"name": "Coder", "role": "coder", "route": "glm",
	})
	run := postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": bot["id"], "prompt": "tighten the API", "project": id,
	})
	if run["project_id"] != id {
		t.Fatalf("run project_id = %v", run["project_id"])
	}
	scope, _ := run["scope_paths"].([]any)
	if len(scope) != 1 || scope[0] != "internal/server" {
		t.Fatalf("run scope_paths = %v", run["scope_paths"])
	}
	prompt, _ := run["prompt"].(string)
	if !strings.Contains(prompt, "SCOPE") || !strings.Contains(prompt, "internal/server") {
		t.Fatalf("run prompt has no scope: %q", prompt)
	}

	// A run pointed at a tag inherits the scope of every project wearing it.
	tagged := postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": bot["id"], "prompt": "work the api", "tag": "api",
	})
	if tagged["tag"] != "api" {
		t.Fatalf("run tag = %v", tagged["tag"])
	}
	tagScope, _ := tagged["scope_paths"].([]any)
	if len(tagScope) != 1 {
		t.Fatalf("tag scope_paths = %v", tagged["scope_paths"])
	}

	// An unknown tag is a 404, not a silent unscoped run.
	bad := postJSONStatus(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": bot["id"], "prompt": "work the void", "tag": "nope",
	})
	if bad.code != 404 {
		t.Fatalf("unknown tag code = %d body = %v", bad.code, bad.body)
	}

	// The project can be removed again.
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/projects/"+id, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("delete = %d", resp.StatusCode)
	}
	empty := getJSON(t, ts, "/api/v1/projects")
	eps, _ := empty["projects"].([]any)
	if len(eps) != 0 {
		t.Fatalf("projects after delete = %v", empty["projects"])
	}
}
