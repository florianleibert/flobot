package server

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/routes"
	"github.com/florianleibert/flobot/internal/schedule"
	"github.com/florianleibert/flobot/internal/skills"
	"github.com/florianleibert/flobot/internal/store"
)

// fireServer builds a server whose state and fire ledger live in a temp dir, so
// one test's occurrences cannot dedupe another's.
func fireServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New()
	st.SetPersistPath(filepath.Join(dir, "state.json"))
	srv := New(st,
		WithExec(&executor.Exec{Bin: bin}),
		WithWorkDir(dir),
		WithHome(dir),
	)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

// seedRoutine creates a bot, a live skill and an enabled routine over them,
// returning the routine. draft makes the skill a draft instead.
func seedRoutine(t *testing.T, srv *Server, draft bool) store.Routine {
	t.Helper()
	routes.SetLocalPin("glm-5.2")
	t.Cleanup(func() { routes.SetLocalPin("") })

	bot, err := srv.store.CreateBot(Actor, "B", "r", "glm")
	if err != nil {
		t.Fatal(err)
	}
	c := skills.Contract{
		Name: "s", Body: "how-to", WhenToUse: "w",
		Procedure: "p", Pitfalls: "x", Verification: "v",
	}
	var sk store.Skill
	if draft {
		sk, err = srv.store.DraftSkillFromRun(Actor, "run-seed", c)
	} else {
		sk, err = srv.store.CreateSkillContract(Actor, c)
	}
	if err != nil {
		t.Fatal(err)
	}
	rt, err := srv.store.CreateRoutine(Actor, bot.ID, sk.ID, "every 5m", "")
	if err != nil {
		t.Fatal(err)
	}
	// A routine must be test-run before it can be enabled: enabling an
	// unproven automation is exactly what that gate prevents.
	if _, err := srv.store.TestRun(rt.ID, Actor, "run-seed", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.EnableRoutine(rt.ID, Actor); err != nil {
		t.Fatal(err)
	}
	rt, err = srv.store.GetRoutine(rt.ID, Actor)
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

// M4 acceptance: firing the same webhook delivery twice runs once.
//
// A provider that did not see our 2xx retries with the same delivery id. Before
// fire identity that retry was a second run — and a second bill.
func TestIntegrationFireDedupesRetriedDelivery(t *testing.T) {
	srv, ts := fireServer(t)
	rt := seedRoutine(t, srv, false)

	id, secret := createIntegration(t, ts, "webhook", "retry me", "bearer")
	postJSON(t, ts, "/api/v1/routines/"+rt.ID+"/integration", map[string]any{"integration_id": id})

	headers := map[string]string{
		"Authorization": "Bearer " + secret,
		"webhook-id":    "delivery-abc",
	}
	code, body := fireWith(t, ts, "/api/v1/integrations/"+id+"/fire", `{"text":"first"}`, headers)
	if code != 200 {
		t.Fatalf("first fire -> %d %s", code, body)
	}
	var first struct {
		Fired int      `json:"fired"`
		Runs  []string `json:"runs"`
	}
	if err := json.Unmarshal([]byte(body), &first); err != nil {
		t.Fatalf("first fire body %q: %v", body, err)
	}
	if first.Fired != 1 || len(first.Runs) != 1 {
		t.Fatalf("first fire = %+v, want one run", first)
	}
	waitRuns(t, ts, body)

	// The retry: same delivery id. It must run nothing.
	code, body = fireWith(t, ts, "/api/v1/integrations/"+id+"/fire", `{"text":"first"}`, headers)
	if code != 202 {
		t.Fatalf("retried fire -> %d %s, want 202 (accepted, nothing run)", code, body)
	}
	var retry struct {
		Fired int      `json:"fired"`
		Runs  []string `json:"runs"`
	}
	if err := json.Unmarshal([]byte(body), &retry); err != nil {
		t.Fatalf("retry body %q: %v", body, err)
	}
	if retry.Fired != 0 || len(retry.Runs) != 0 {
		t.Fatalf("a retried delivery ran again: %+v", retry)
	}

	// A genuinely new delivery still runs.
	headers["webhook-id"] = "delivery-def"
	code, body = fireWith(t, ts, "/api/v1/integrations/"+id+"/fire", `{"text":"second"}`, headers)
	if code != 200 {
		t.Fatalf("new delivery -> %d %s", code, body)
	}
	var next struct {
		Fired int `json:"fired"`
	}
	if err := json.Unmarshal([]byte(body), &next); err != nil {
		t.Fatalf("new delivery body %q: %v", body, err)
	}
	if next.Fired != 1 {
		t.Fatalf("a new delivery was refused: %+v", next)
	}
}

// A delivery with no provider id has no identity, so it must run: a missing id
// must never swallow a real event.
func TestIntegrationFireWithoutDeliveryIDAlwaysRuns(t *testing.T) {
	srv, ts := fireServer(t)
	rt := seedRoutine(t, srv, false)

	id, secret := createIntegration(t, ts, "webhook", "no id", "bearer")
	postJSON(t, ts, "/api/v1/routines/"+rt.ID+"/integration", map[string]any{"integration_id": id})

	for i := 0; i < 2; i++ {
		code, body := fireWith(t, ts, "/api/v1/integrations/"+id+"/fire", `{"text":"x"}`, map[string]string{
			"Authorization": "Bearer " + secret,
		})
		if code != 200 {
			t.Fatalf("fire %d -> %d %s", i, code, body)
		}
		var res struct {
			Fired int `json:"fired"`
		}
		if err := json.Unmarshal([]byte(body), &res); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if res.Fired != 1 {
			t.Fatalf("fire %d fired = %d, want 1 (no delivery id must not dedupe)", i, res.Fired)
		}
		waitRuns(t, ts, body)
	}
}

// M4 acceptance: a routine whose route cannot be pinned fails validation with
// zero tokens spent — no run is created and the occurrence is not consumed.
func TestFireValidationRefusesUnpinnedRoute(t *testing.T) {
	srv, _ := fireServer(t)
	routes.SetLocalPin("") // the local route is unpinned

	bot, err := srv.store.CreateBot(Actor, "L", "r", "local")
	if err != nil {
		t.Fatal(err)
	}
	sk, err := srv.store.CreateSkill(Actor, "s", "how-to")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := srv.store.CreateRoutine(Actor, bot.ID, sk.ID, "every 5m", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.TestRun(stored.ID, Actor, "run-seed", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.EnableRoutine(stored.ID, Actor); err != nil {
		t.Fatal(err)
	}
	routine, err := srv.store.GetRoutine(stored.ID, Actor)
	if err != nil {
		t.Fatal(err)
	}

	runsBefore := len(srv.store.ListRuns(Actor))
	runID, err := srv.dispatchFire(fireRequest{Routine: routine, FireID: "unpinned-1", Scheduled: true})
	if err == nil {
		t.Fatal("an unpinned route was dispatched")
	}
	if runID != "" {
		t.Fatalf("an unpinned route produced run %q", runID)
	}
	if got := len(srv.store.ListRuns(Actor)); got != runsBefore {
		t.Fatalf("runs went from %d to %d: validation must not create a run", runsBefore, got)
	}
	if srv.gate().fires.Seen("unpinned-1") {
		t.Fatal("a refused fire consumed its occurrence")
	}
}

// M4 acceptance: a draft skill cannot fire a routine. This is the gate that
// makes "learn from a run" safe.
func TestFireValidationRefusesDraftSkill(t *testing.T) {
	srv, _ := fireServer(t)
	routine := seedRoutine(t, srv, true)

	runsBefore := len(srv.store.ListRuns(Actor))
	runID, err := srv.dispatchFire(fireRequest{Routine: routine, FireID: "draft-1", Scheduled: true})
	if err != nil {
		t.Fatalf("a draft skill is a permanent skip, not an error: %v", err)
	}
	if runID != "" {
		t.Fatalf("a draft skill produced run %q", runID)
	}
	if got := len(srv.store.ListRuns(Actor)); got != runsBefore {
		t.Fatalf("a draft skill created a run (%d -> %d)", runsBefore, got)
	}
	if srv.gate().fires.Seen("draft-1") {
		t.Fatal("a refused fire consumed its occurrence")
	}
}

// The global stop refuses every fire without touching per-routine state.
func TestGlobalStopRefusesFires(t *testing.T) {
	srv, _ := fireServer(t)
	routine := seedRoutine(t, srv, false)

	srv.StopAllFires()
	if !srv.FiresStopped() {
		t.Fatal("stop did not engage")
	}
	runsBefore := len(srv.store.ListRuns(Actor))
	if _, err := srv.dispatchFire(fireRequest{Routine: routine, FireID: "stopped-1", Scheduled: true}); err != nil {
		t.Fatalf("stopped fire returned an error: %v", err)
	}
	if got := len(srv.store.ListRuns(Actor)); got != runsBefore {
		t.Fatalf("a stopped server still fired (%d -> %d)", runsBefore, got)
	}

	srv.ResumeFires()
	if srv.FiresStopped() {
		t.Fatal("resume did not lift the stop")
	}
}

// The ledger survives a restart: an occurrence that fired before the process
// restarted must not fire again after it.
func TestFireLedgerSurvivesRestart(t *testing.T) {
	srv, _ := fireServer(t)
	routine := seedRoutine(t, srv, false)

	if _, err := srv.dispatchFire(fireRequest{Routine: routine, FireID: "restart-1", Scheduled: true}); err != nil {
		t.Fatalf("first fire: %v", err)
	}

	fresh := New(store.New(), WithHome(srv.home))
	if !fresh.gate().fires.Seen("restart-1") {
		t.Fatal("the ledger forgot an occurrence across a restart")
	}
}

// The ledger endpoint reports what fired and whether the global stop is on.
func TestFireLedgerEndpoint(t *testing.T) {
	srv, ts := fireServer(t)
	routine := seedRoutine(t, srv, false)

	if _, err := srv.dispatchFire(fireRequest{Routine: routine, FireID: "ledger-1", Scheduled: true}); err != nil {
		t.Fatalf("fire: %v", err)
	}

	res := getJSON(t, ts, "/api/v1/fires")
	if res["schema"] != "flobot.fires.v1" {
		t.Fatalf("schema = %v", res["schema"])
	}
	fires, _ := res["fires"].([]any)
	found := false
	for _, f := range fires {
		m, _ := f.(map[string]any)
		if m["id"] == "ledger-1" {
			found = true
			if m["routine_id"] != routine.ID {
				t.Errorf("routine_id = %v", m["routine_id"])
			}
			if m["run_id"] == "" || m["run_id"] == nil {
				t.Error("ledger record has no run id")
			}
			if m["at"] == "" || m["at"] == nil {
				t.Error("ledger record has no timestamp")
			}
			if _, err := time.Parse(time.RFC3339, m["at"].(string)); err != nil {
				t.Errorf("at = %v: %v", m["at"], err)
			}
			break
		}
	}
	if !found {
		t.Fatalf("ledger-1 missing from %v", fires)
	}
	if res["stopped"] != false {
		t.Errorf("stopped = %v", res["stopped"])
	}

	if out := postJSON(t, ts, "/api/v1/fires/stop", map[string]any{}); out["stopped"] != true {
		t.Fatalf("stop = %v", out)
	}
	if res := getJSON(t, ts, "/api/v1/fires"); res["stopped"] != true {
		t.Fatalf("stopped after stop = %v", res["stopped"])
	}
	if out := postJSON(t, ts, "/api/v1/fires/resume", map[string]any{}); out["stopped"] != false {
		t.Fatalf("resume = %v", out)
	}
}

// A rate-limited routine is refused rather than spawning a run per event.
func TestFireRateLimitRefusesBurst(t *testing.T) {
	srv, _ := fireServer(t)
	routine := seedRoutine(t, srv, false)

	g := srv.gate()
	g.limiter = schedule.NewLimiter(1, time.Hour)

	if _, err := srv.dispatchFire(fireRequest{Routine: routine, FireID: "burst-1", Scheduled: true}); err != nil {
		t.Fatalf("first fire: %v", err)
	}
	runsBefore := len(srv.store.ListRuns(Actor))
	if _, err := srv.dispatchFire(fireRequest{Routine: routine, FireID: "burst-2", Scheduled: true}); err != nil {
		t.Fatalf("second fire: %v", err)
	}
	if got := len(srv.store.ListRuns(Actor)); got != runsBefore {
		t.Fatalf("a rate-limited fire still spawned a run (%d -> %d)", runsBefore, got)
	}
}

// A paused routine never fires and never records an occurrence.
func TestPausedRoutineDoesNotFire(t *testing.T) {
	srv, _ := fireServer(t)
	routine := seedRoutine(t, srv, false)
	if _, err := srv.store.PauseRoutine(routine.ID, Actor, true); err != nil {
		t.Fatal(err)
	}
	routine.Paused = true

	runsBefore := len(srv.store.ListRuns(Actor))
	if _, err := srv.dispatchFire(fireRequest{Routine: routine, FireID: "paused-1", Scheduled: true}); err != nil {
		t.Fatalf("paused fire returned an error: %v", err)
	}
	if got := len(srv.store.ListRuns(Actor)); got != runsBefore {
		t.Fatalf("a paused routine fired (%d -> %d)", runsBefore, got)
	}
	if srv.gate().fires.Seen("paused-1") {
		t.Fatal("a paused routine consumed its occurrence")
	}
}
