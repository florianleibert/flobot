package server

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/florianleibert/flobot/internal/store"
)

// TestRoutineTriggerValidation proves that a schedule which announces itself
// as a cron or an interval is checked before it is stored. A typo'd cron used
// to be accepted and then silently treated as manual — the routine never
// fired and nothing said why.
func TestRoutineTriggerValidation(t *testing.T) {
	dir := t.TempDir()
	st := store.New()
	st.SetPersistPath(filepath.Join(dir, "state.json"))
	srv := New(st, WithWorkDir(dir))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{"name": "S", "role": "r", "route": "glm"})
	sk := postJSON(t, ts, "/api/v1/skills", map[string]any{"name": "s", "body": "how-to"})

	cases := []struct {
		trigger string
		wantErr bool
	}{
		{"cron 0 9 * * *", false},
		{"cron @daily", false},
		{"cron 0 9 * * 1-5", false},
		{"every 5m", false},
		{"manual", false},
		{"cron nonsense * * *", true},
		{"cron 99 * * *", true},
		{"every 5x", true},
	}
	for _, tc := range cases {
		res := postJSONStatus(t, ts, "/api/v1/routines", map[string]any{
			"bot_id": bot["id"], "skill_id": sk["id"], "trigger": tc.trigger,
		})
		if tc.wantErr {
			if res.code != 400 {
				t.Errorf("trigger %q: code = %d, want 400 (body %s)", tc.trigger, res.code, res.body)
			}
			if !strings.Contains(res.body, "invalid") {
				t.Errorf("trigger %q: body = %q, want it to say why", tc.trigger, res.body)
			}
			continue
		}
		if res.code < 200 || res.code > 299 {
			t.Errorf("trigger %q: code = %d body = %s", tc.trigger, res.code, res.body)
		}
	}
}
