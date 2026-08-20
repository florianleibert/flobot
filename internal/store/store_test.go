package store

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestRosterOwnershipAndNoSecrets(t *testing.T) {
	st := New()
	chief, err := st.CreateBot("sess-a", "Chief", "chief-of-staff", "muse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(chief.ID, "bot-") {
		t.Fatalf("bot id = %s", chief.ID)
	}
	coder, err := st.CreateBot("sess-a", "Coder", "coder", "qwen")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateBot("sess-a", "Bad", "x", "nope"); err == nil {
		t.Fatal("unknown route must fail")
	}
	if _, err := st.Remember(chief.ID, "sess-b", "stolen"); err != ErrDenied {
		t.Fatalf("stranger memory: %v", err)
	}
	if _, err := st.Remember(chief.ID, "sess-a", "annual contracts only"); err != nil {
		t.Fatal(err)
	}
	sk, err := st.CreateSkill("sess-a", "sysinfo", "Run read-only sysctl/uname. Never mutate network or tmux.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sk.ID, "skill-") {
		t.Fatalf("skill id = %s", sk.ID)
	}
	rt, err := st.CreateRoutine("sess-a", coder.ID, sk.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rt.ID, "rtn-") {
		t.Fatalf("rtn id = %s", rt.ID)
	}
	if !rt.Paused {
		t.Fatal("new routine must start paused")
	}
	if _, err := st.EnableRoutine(rt.ID, "sess-a"); err == nil {
		t.Fatal("enable without test-run must fail")
	}
	run, err := st.CreateRun("sess-a", coder.ID, "uname", "ask", false, false, "", "qwen-3.8", "qwen")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(run.ID, "run-") {
		t.Fatalf("run id = %s", run.ID)
	}
	got, err := st.TestRun(rt.ID, "sess-a", run.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.LastRun == "" {
		t.Fatal("LastRun unset")
	}
	if !strings.Contains(got.LastNote, run.ID) {
		t.Fatalf("LastNote should include run id: %q", got.LastNote)
	}
	if strings.Contains(got.LastNote, "no external effect") {
		t.Fatalf("store must not claim no external effect: %q", got.LastNote)
	}
	if _, err := st.EnableRoutine(rt.ID, "sess-a"); err != nil {
		t.Fatal(err)
	}
	msg, err := st.Handoff("sess-a", chief.ID, coder.ID, "count Go files")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(msg.ID, "msg-") {
		t.Fatalf("msg id = %s", msg.ID)
	}
	if msg.OwnerBot != coder.ID {
		t.Fatalf("handoff owner = %s", msg.OwnerBot)
	}
	if n := len(st.ListBots("sess-b")); n != 0 {
		t.Fatalf("stranger saw %d bots", n)
	}
	raw, err := json.Marshal(struct {
		Bots     []Bot     `json:"bots"`
		Skills   []Skill   `json:"skills"`
		Routines []Routine `json:"routines"`
		Messages []Message `json:"messages"`
		Runs     []Run     `json:"runs"`
	}{
		Bots:     st.ListBots("sess-a"),
		Skills:   st.ListSkills("sess-a"),
		Routines: st.ListRoutines("sess-a"),
		Messages: st.ListMessages("sess-a"),
		Runs:     st.ListRuns("sess-a"),
	})
	if err != nil {
		t.Fatal(err)
	}
	low := strings.ToLower(string(raw))
	for _, leak := range []string{`"token"`, "refresh", "password", "bearer", "api_key", "sess-a", `"owner"`} {
		if strings.Contains(low, leak) {
			t.Fatalf("public JSON leaked %q: %s", leak, raw)
		}
	}
}

func TestHandoffDoesNotClobberConcurrentMessages(t *testing.T) {
	st := New()
	a, err := st.CreateBot("sess-a", "Chief", "chief-of-staff", "muse")
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateBot("sess-a", "Coder", "coder", "qwen")
	if err != nil {
		t.Fatal(err)
	}
	const iters = 500
	var wg sync.WaitGroup
	for i := 0; i < iters; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := st.Message("sess-a", a.ID, b.ID, "", "plain message"); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := st.Handoff("sess-a", a.ID, b.ID, "handoff message"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	msgs := st.ListMessages("sess-a")
	if len(msgs) != 2*iters {
		t.Fatalf("message count = %d, want %d", len(msgs), 2*iters)
	}
	var plain, handoff int
	for _, msg := range msgs {
		switch msg.Body {
		case "plain message":
			plain++
		case "handoff message":
			handoff++
			if msg.OwnerBot != b.ID {
				t.Fatalf("handoff owner = %s, want %s", msg.OwnerBot, b.ID)
			}
		default:
			t.Fatalf("unexpected body %q", msg.Body)
		}
	}
	if plain != iters || handoff != iters {
		t.Fatalf("plain=%d handoff=%d, want %d each (a handoff clobbered a concurrent message)", plain, handoff, iters)
	}
}

func TestHandoffRejectsBroadcast(t *testing.T) {
	st := New()
	a, err := st.CreateBot("sess-a", "Chief", "chief-of-staff", "muse")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Handoff("sess-a", a.ID, "@all", "who owns this?"); err == nil {
		t.Fatal("handoff to @all must fail: broadcast has no single owner")
	}
}

func TestMessagesAreInvisibleToStrangers(t *testing.T) {
	st := New()
	a, err := st.CreateBot("sess-a", "Chief", "chief-of-staff", "muse")
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateBot("sess-a", "Coder", "coder", "qwen")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Message("sess-a", a.ID, "@all", "", "broadcast"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Handoff("sess-a", a.ID, b.ID, "work item"); err != nil {
		t.Fatal(err)
	}
	if n := len(st.ListMessages("sess-b")); n != 0 {
		t.Fatalf("stranger saw %d messages", n)
	}
}

func TestRunEventsCapAndClip(t *testing.T) {
	st := New()
	bot, err := st.CreateBot("local", "Coder", "coder", "glm")
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateRun("local", bot.ID, "hi", "code", false, false, "", "glm-5.2", "glm")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendEvent(run.ID, "sess-b", Event{Kind: "content", Text: "x"}); err != ErrDenied {
		t.Fatalf("stranger append: %v", err)
	}
	big := strings.Repeat("a", textCap+50)
	if err := st.AppendEvent(run.ID, "local", Event{Kind: "content", Text: big}); err != nil {
		t.Fatal(err)
	}
	evs, err := st.ListEvents(run.ID, "local")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || len(evs[0].Text) != textCap {
		t.Fatalf("clip: n=%d len=%d", len(evs), len(evs[0].Text))
	}
	for i := 0; i < maxEvents+10; i++ {
		if err := st.AppendEvent(run.ID, "local", Event{Kind: "content", Text: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.AppendEvent(run.ID, "local", Event{Kind: "done"}); err != nil {
		t.Fatal(err)
	}
	evs, err = st.ListEvents(run.ID, "local")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != maxEvents {
		t.Fatalf("cap = %d", len(evs))
	}
	if evs[len(evs)-1].Kind != "done" {
		t.Fatalf("terminal event dropped: %s", evs[len(evs)-1].Kind)
	}
	if err := st.FinishRun(run.ID, "local", "done", ""); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetRun(run.ID, "local")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "done" || got.EndedAt == "" {
		t.Fatalf("%+v", got)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"events"`) {
		t.Fatalf("events must be json-omitted: %s", raw)
	}
}
