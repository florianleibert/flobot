package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// patchJSONStatus issues a PATCH with a JSON body and returns the status
// code and the response body.
func patchJSONStatus(t *testing.T, ts *httptest.Server, path string, body map[string]any) struct {
	code int
	body string
} {
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
	b := make([]byte, 4096)
	n, _ := resp.Body.Read(b)
	return struct {
		code int
		body string
	}{resp.StatusCode, string(b[:n])}
}

// newRoutineServer returns a server with one skill and one routine. The
// routine's id is the second return value.
func newRoutineServer(t *testing.T, trigger string) (*httptest.Server, string) {
	t.Helper()
	home := t.TempDir()
	srv := New(nil, WithHome(home), WithWorkDir(t.TempDir()))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	sk := postJSON(t, ts, "/api/v1/skills", map[string]any{
		"name": "reviewer",
		"body": "review the diff",
	})
	skillID, _ := sk["id"].(string)
	if skillID == "" {
		t.Fatalf("skill create returned no id: %v", sk)
	}
	rt := postJSON(t, ts, "/api/v1/routines", map[string]any{
		"bot_id":   "bot-glm",
		"skill_id": skillID,
		"trigger":  trigger,
	})
	id, _ := rt["id"].(string)
	if id == "" {
		t.Fatalf("routine create returned no id: %v", rt)
	}
	return ts, id
}

// findRoutine returns the roster's routine with the given id. The tests in
// this package share one state file, so the roster also holds routines
// created by other tests — the routine count is not a fixed number, so the
// routine is looked up by id.
func findRoutine(t *testing.T, ts *httptest.Server, id string) map[string]any {
	t.Helper()
	roster := getJSON(t, ts, "/api/v1/roster")
	routines, _ := roster["routines"].([]any)
	for _, r := range routines {
		rt, _ := r.(map[string]any)
		if rt["id"] == id {
			return rt
		}
	}
	t.Fatalf("routine %s not found in the roster", id)
	return nil
}

func TestPatchRoutineChangesTrigger(t *testing.T) {
	ts, id := newRoutineServer(t, "cron 0 9 * * *")

	res := patchJSONStatus(t, ts, "/api/v1/routines/"+id, map[string]any{
		"trigger": "every 30m",
	})
	if res.code != http.StatusOK {
		t.Fatalf("code = %d body = %s, want 200", res.code, res.body)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.body), &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, res.body)
	}
	if out["trigger"] != "every 30m" {
		t.Fatalf("trigger = %v, want %q", out["trigger"], "every 30m")
	}
	next, _ := out["next_fire"].(string)
	if next == "" {
		t.Fatalf("no next_fire in the response: %v", out)
	}
	if _, err := time.Parse(time.RFC3339, next); err != nil {
		t.Fatalf("next_fire %q is not RFC3339: %v", next, err)
	}
}

func TestPatchRoutineKeepsOtherFields(t *testing.T) {
	ts, id := newRoutineServer(t, "every 5m")

	res := patchJSONStatus(t, ts, "/api/v1/routines/"+id, map[string]any{
		"trigger": "every 10m",
	})
	if res.code != http.StatusOK {
		t.Fatalf("code = %d body = %s, want 200", res.code, res.body)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.body), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The trigger changed; the routine's identity must not.
	if out["id"] != id {
		t.Fatalf("id = %v, want %q", out["id"], id)
	}
	if out["bot_id"] != "bot-glm" {
		t.Fatalf("bot_id = %v, want bot-glm", out["bot_id"])
	}
	if out["skill_id"] == "" {
		t.Fatalf("skill_id lost: %v", out)
	}
}

func TestPatchRoutineRejectsUnparseableTrigger(t *testing.T) {
	ts, id := newRoutineServer(t, "every 5m")

	res := patchJSONStatus(t, ts, "/api/v1/routines/"+id, map[string]any{
		"trigger": "cron 09 * *",
	})
	if res.code != http.StatusBadRequest {
		t.Fatalf("code = %d body = %s, want 400", res.code, res.body)
	}
	// The bad trigger must not have been stored.
	rt := findRoutine(t, ts, id)
	if rt["trigger"] != "every 5m" {
		t.Fatalf("trigger = %v, want the unchanged %q", rt["trigger"], "every 5m")
	}
}

func TestPatchRoutineRejectsEmptyTrigger(t *testing.T) {
	ts, id := newRoutineServer(t, "every 5m")
	res := patchJSONStatus(t, ts, "/api/v1/routines/"+id, map[string]any{
		"trigger": "   ",
	})
	if res.code != http.StatusBadRequest {
		t.Fatalf("code = %d body = %s, want 400", res.code, res.body)
	}
}

func TestPatchRoutineUnknownIDIs404(t *testing.T) {
	ts, _ := newRoutineServer(t, "every 5m")
	res := patchJSONStatus(t, ts, "/api/v1/routines/rtn-does-not-exist", map[string]any{
		"trigger": "every 10m",
	})
	if res.code != http.StatusNotFound {
		t.Fatalf("code = %d body = %s, want 404", res.code, res.body)
	}
}

func TestPatchRoutineWithNothingToUpdateIs400(t *testing.T) {
	ts, id := newRoutineServer(t, "every 5m")
	res := patchJSONStatus(t, ts, "/api/v1/routines/"+id, map[string]any{})
	if res.code != http.StatusBadRequest {
		t.Fatalf("code = %d body = %s, want 400", res.code, res.body)
	}
}

func TestRosterCarriesNextFire(t *testing.T) {
	ts, id := newRoutineServer(t, "cron 0 9 * * *")
	rt := findRoutine(t, ts, id)
	next, _ := rt["next_fire"].(string)
	if next == "" {
		t.Fatalf("routine has no next_fire: %v", rt)
	}
	if _, err := time.Parse(time.RFC3339, next); err != nil {
		t.Fatalf("next_fire %q is not RFC3339: %v", next, err)
	}
}

func TestRosterManualRoutineHasNoNextFire(t *testing.T) {
	ts, id := newRoutineServer(t, "manual")
	rt := findRoutine(t, ts, id)
	if next, ok := rt["next_fire"]; ok && next != "" {
		t.Fatalf("next_fire = %v, want absent for a manual routine", next)
	}
}
