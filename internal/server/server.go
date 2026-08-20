// Package server is the standalone loopback HTTP API and static UI.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/routes"
	"github.com/florianleibert/flobot/internal/store"
	"github.com/florianleibert/flobot/web"
)

const (
	Actor          = store.ActorLocal
	defaultAddr    = "127.0.0.1:4120"
	runBodyLimit   = 64 << 10
	smallBodyLimit = 8 << 10
)

// Server serves roster + runs on loopback.
type Server struct {
	store   *store.Store
	exec    *executor.Exec
	index   string
	probes  []Probe
	client  *http.Client
	home    string
	workDir string
}

type Option func(*Server)

func WithExec(e *executor.Exec) Option { return func(s *Server) { s.exec = e } }
func WithIndex(html string) Option     { return func(s *Server) { s.index = html } }
func WithProbes(p []Probe) Option      { return func(s *Server) { s.probes = p } }
func WithHTTPClient(c *http.Client) Option {
	return func(s *Server) { s.client = c }
}
func WithHome(home string) Option { return func(s *Server) { s.home = home } }
func WithWorkDir(dir string) Option {
	return func(s *Server) { s.workDir = dir }
}

func New(st *store.Store, opts ...Option) *Server {
	s := &Server{
		store: st,
		exec:  &executor.Exec{},
		index: web.IndexHTML,
	}
	for _, o := range opts {
		o(s)
	}
	if s.store == nil {
		s.store = store.New()
	}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.indexHTML)
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /api/v1/roster", s.roster)
	mux.HandleFunc("POST /api/v1/bots", s.createBot)
	mux.HandleFunc("POST /api/v1/handoff", s.handoff)
	mux.HandleFunc("POST /api/v1/skills", s.createSkill)
	mux.HandleFunc("POST /api/v1/routines", s.createRoutine)
	mux.HandleFunc("POST /api/v1/routines/{id}/test-run", s.testRun)
	mux.HandleFunc("POST /api/v1/routines/{id}/enable", s.enableRoutine)
	mux.HandleFunc("POST /api/v1/runs", s.createRun)
	mux.HandleFunc("GET /api/v1/runs", s.listRuns)
	mux.HandleFunc("GET /api/v1/runs/{id}", s.getRun)
	mux.HandleFunc("GET /api/v1/runs/{id}/events", s.runEvents)
	mux.HandleFunc("GET /api/v1/health/models", s.healthModels)
	return mux
}

func DefaultAddr() string { return defaultAddr }

func (s *Server) ListenAndServe(addr string) error {
	if addr == "" {
		addr = defaultAddr
	}
	if err := CheckLoopbackAddr(addr); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.Serve(ln)
}

func (s *Server) indexHTML(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, s.index)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "service": "flobot"})
}

func (s *Server) roster(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"schema":       "flobot.v1",
		"actor":        Actor,
		"install_free": true,
		"bots":         s.store.ListBots(Actor),
		"skills":       s.store.ListSkills(Actor),
		"routines":     s.store.ListRoutines(Actor),
		"messages":     s.store.ListMessages(Actor),
		"runs":         s.store.ListRuns(Actor),
		"routes":       routes.DefaultRoutes(),
	})
}

func (s *Server) createBot(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Role  string `json:"role"`
		Route string `json:"route"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	bot, err := s.store.CreateBot(Actor, body.Name, body.Role, body.Route)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, bot)
}

func (s *Server) handoff(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	msg, err := s.store.Handoff(Actor, body.From, body.To, body.Body)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, msg)
}

func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, runBodyLimit, &body) {
		return
	}
	sk, err := s.store.CreateSkill(Actor, body.Name, body.Body)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, sk)
}

func (s *Server) createRoutine(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BotID   string `json:"bot_id"`
		SkillID string `json:"skill_id"`
		Trigger string `json:"trigger"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	rt, err := s.store.CreateRoutine(Actor, body.BotID, body.SkillID, body.Trigger)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, rt)
}

func (s *Server) testRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rt, err := s.store.GetRoutine(id, Actor)
	if err != nil {
		httpError(w, err)
		return
	}
	bot, err := s.store.GetBot(rt.BotID, Actor)
	if err != nil {
		httpError(w, err)
		return
	}
	sk, err := s.store.GetSkill(rt.SkillID, Actor)
	if err != nil {
		httpError(w, err)
		return
	}
	pin, err := routes.ResolvePin(bot.Route)
	if err != nil {
		httpError(w, err)
		return
	}
	prompt := "Test-run the following skill. Ask/read only; do not mutate the workspace.\n\n" + sk.Body
	run, err := s.store.CreateRun(Actor, bot.ID, prompt, "ask", false, false, "", pin, bot.Route)
	if err != nil {
		httpError(w, err)
		return
	}
	rt, err = s.store.TestRun(rt.ID, Actor, run.ID, "")
	if err != nil {
		httpError(w, err)
		return
	}
	s.spawn(run, executor.Request{
		Prompt:  prompt,
		Pin:     pin,
		Intent:  "ask",
		Yolo:    false,
		Verify:  false,
		Route:   bot.Route,
		Timeout: executor.AskTimeout,
		Dir:     s.workDir,
	})
	writeJSON(w, map[string]any{
		"id":        rt.ID,
		"bot_id":    rt.BotID,
		"skill_id":  rt.SkillID,
		"trigger":   rt.Trigger,
		"paused":    rt.Paused,
		"last_run":  rt.LastRun,
		"last_note": rt.LastNote,
		"run_id":    run.ID,
		"run":       run,
	})
}

func (s *Server) enableRoutine(w http.ResponseWriter, r *http.Request) {
	rt, err := s.store.EnableRoutine(r.PathValue("id"), Actor)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, rt)
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BotID    string `json:"bot_id"`
		Prompt   string `json:"prompt"`
		Intent   string `json:"intent"`
		Yolo     bool   `json:"yolo"`
		Verify   bool   `json:"verify"`
		Worktree string `json:"worktree"`
	}
	if !decodeJSON(w, r, runBodyLimit, &body) {
		return
	}
	bot, err := s.store.GetBot(body.BotID, Actor)
	if err != nil {
		httpError(w, err)
		return
	}
	pin, err := routes.ResolvePin(bot.Route)
	if err != nil {
		httpError(w, err)
		return
	}
	intent := strings.TrimSpace(body.Intent)
	if intent == "" {
		intent = "code"
	}
	run, err := s.store.CreateRun(Actor, bot.ID, body.Prompt, intent, body.Yolo, body.Verify, body.Worktree, pin, bot.Route)
	if err != nil {
		httpError(w, err)
		return
	}
	s.spawn(run, executor.Request{
		Prompt:   body.Prompt,
		Pin:      pin,
		Intent:   intent,
		Yolo:     body.Yolo,
		Verify:   body.Verify,
		Worktree: body.Worktree,
		Route:    bot.Route,
		Dir:      s.workDir,
	})
	writeJSON(w, run)
}

func (s *Server) listRuns(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"runs": s.store.ListRuns(Actor)})
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetRun(r.PathValue("id"), Actor)
	if err != nil {
		httpError(w, err)
		return
	}
	run.Events = nil
	writeJSON(w, run)
}

func (s *Server) runEvents(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetRun(r.PathValue("id"), Actor)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"id":     run.ID,
		"status": run.Status,
		"error":  run.Err,
		"events": run.Events,
	})
}

func (s *Server) spawn(run store.Run, req executor.Request) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				_ = s.store.FinishRun(run.ID, Actor, "error", "internal error")
			}
		}()
		res := s.exec.Run(context.Background(), req, func(ev executor.Event) {
			_ = s.store.AppendEvent(run.ID, Actor, store.Event{
				Kind:    ev.Kind,
				TurnID:  ev.TurnID,
				Text:    ev.Text,
				Tool:    ev.Tool,
				IsError: ev.IsError,
			})
		})
		status := "done"
		errText := ""
		if res.Err != nil {
			status = "error"
			errText = res.Err.Error()
		}
		_ = s.store.FinishRun(run.ID, Actor, status, errText)
	}()
}

func decodeJSON(w http.ResponseWriter, r *http.Request, limit int64, dst any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, limit)).Decode(dst); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return false
	}
	return true
}

func httpError(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	if errors.Is(err, store.ErrDenied) {
		code = http.StatusForbidden
	}
	if errors.Is(err, routes.ErrLocalUnpinned) {
		code = http.StatusBadRequest
	}
	http.Error(w, err.Error(), code)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
