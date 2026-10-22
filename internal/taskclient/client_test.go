package taskclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stubServe is a minimal flocode task API stub for contract tests.
func stubServe(t *testing.T) (*httptest.Server, *stubState) {
	t.Helper()
	st := &stubState{events: make(chan string, 16)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		st.submitted = body
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"task_id": "t-1", "status": "queued"})
	})
	mux.HandleFunc("GET /api/v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task_id": r.PathValue("id"), "status": st.status, "dropped_events": st.dropped,
		})
	})
	mux.HandleFunc("POST /api/v1/tasks/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		st.status = "cancelled"
		_ = json.NewEncoder(w).Encode(map[string]string{"task_id": r.PathValue("id"), "status": "cancelled"})
	})
	mux.HandleFunc("GET /api/v1/tasks/{id}/result", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task_id": r.PathValue("id"), "status": "completed",
			"patch": map[string]any{
				"format": "unified-diff", "sha256": "abc", "changed_files": []string{"a.go"},
				"diff": "--- a/a.go\n+++ b/a.go\n@@ -1,1 +1,1 @@\n-x\n+y\n", "applied": true,
			},
			"confidence":            0.8,
			"requires_human_review": true,
		})
	})
	mux.HandleFunc("GET /api/v1/tasks/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for _, ev := range []string{"tool_call", "tool_result", "completed"} {
			if r.Context().Err() != nil {
				return
			}
			_, _ = w.Write([]byte("event: " + ev + "\ndata: {\"k\":1}\n\n"))
			fl.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, st
}

type stubState struct {
	submitted map[string]any
	status    string
	dropped   int
	events    chan string
}

func TestSubmitStatusResult(t *testing.T) {
	srv, st := stubServe(t)
	st.status = "running"
	c := New(srv.URL, srv.Client())
	sub, err := c.Submit(context.Background(), map[string]any{"goal": "test"})
	if err != nil {
		t.Fatal(err)
	}
	if sub.TaskID != "t-1" || sub.Status != "queued" {
		t.Errorf("submit: %+v", sub)
	}
	if st.submitted["goal"] != "test" {
		t.Errorf("submitted body: %+v", st.submitted)
	}
	s, err := c.Status(context.Background(), "t-1")
	if err != nil || s.Status != "running" {
		t.Errorf("status: %+v err=%v", s, err)
	}
	res, err := c.Result(context.Background(), "t-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Patch.Format != "unified-diff" || !res.RequiresHumanReview {
		t.Errorf("result: %+v", res)
	}
	if !strings.Contains(res.Patch.Diff, "+y") {
		t.Errorf("patch diff: %q", res.Patch.Diff)
	}
}

func TestStream(t *testing.T) {
	srv, _ := stubServe(t)
	c := New(srv.URL, srv.Client())
	ch, err := c.Stream(context.Background(), "t-1")
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for ev := range ch {
		types = append(types, ev.Type)
	}
	if len(types) != 3 || types[0] != "tool_call" || types[2] != "completed" {
		t.Errorf("stream events: %v", types)
	}
}

func TestCancel(t *testing.T) {
	srv, st := stubServe(t)
	c := New(srv.URL, srv.Client())
	if err := c.Cancel(context.Background(), "t-1"); err != nil {
		t.Fatal(err)
	}
	if st.status != "cancelled" {
		t.Errorf("status after cancel: %q", st.status)
	}
}

func TestTerminal(t *testing.T) {
	for s, want := range map[string]bool{
		"completed": true, "cancelled": true, "failed": true, "running": false, "queued": false,
	} {
		if Terminal(s) != want {
			t.Errorf("Terminal(%q)=%v want %v", s, Terminal(s), want)
		}
	}
}
