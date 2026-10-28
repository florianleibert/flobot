// Package server is the standalone loopback HTTP API and static UI.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/florianleibert/flobot/internal/amesh"
	"github.com/florianleibert/flobot/internal/checkpoint"
	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/routes"
	"github.com/florianleibert/flobot/internal/schedule"
	"github.com/florianleibert/flobot/internal/store"
	"github.com/florianleibert/flobot/internal/queue"
	"github.com/florianleibert/flobot/internal/taskclient"
	"github.com/florianleibert/flobot/internal/threads"
	"github.com/florianleibert/flobot/internal/workspace"
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
	taskExec *taskclient.Adapter
	index   string
	probes  []Probe
	client  *http.Client
	home    string
	workDir string
	// workMu guards workDir/ws: the workspace can be switched at runtime
	// via POST /api/v1/workspace/select (Code Studio folder picker).
	workMu sync.RWMutex
	// ameshAddr overrides the amesh-web loopback address for mesh routes.
	ameshAddr string
	// sched fires enabled "every <duration>" routines. Nil until StartScheduler.
	sched *schedule.Scheduler
	// cancels holds the cancel func for each running run so a client can
	// stop a runaway run; spawn deletes its entry when the run finishes.
	cancels   map[string]context.CancelFunc
	cancelsMu sync.Mutex
	// stopped records runs cancelled via POST /api/v1/runs/{id}/stop so their
	// terminal status reads "stopped" instead of a misleading "error".
	stopped map[string]bool

	hubOnce sync.Once
	hub     *hub

	// ws is the lazily-initialized workspace view over workDir (Code Studio).
	ws   *workspace.Workspace
	wsMu sync.Mutex

	// cp is the checkpoint store; rs the persisted run summaries; snaps
	// the in-memory pre/post-run git trees (Code Studio Phase 2).
	cp     *checkpoint.Store
	cpMu   sync.Mutex
	rs     *store.RunStore
	rsOnce sync.Once
	snaps  *runSnapshots
	// revMu serializes diff-review mutations (apply/reject) so two
	// concurrent reviews cannot interleave workspace writes.
	revMu sync.Mutex
	// contracts stashes result-contract payloads that arrive (via the
	// event stream) before capturePostRun writes the run summary.
	contracts   map[string]contractPayload
	contractsMu sync.Mutex
	// threads owns the thread worktrees, queue serves the review queue,
	// and live holds the in-flight diff of running runs. The thread store
	// lives in its own file so state.json's "never persists runs" contract
	// stays true.
	threads *threads.Manager
	queue   *queue.Queue
	live    *liveStore
	// cpRoots caches one checkpoint store per git root, so a run in a
	// worktree snapshots its own tree while the user's tree stays the
	// default.
	cpRoots map[string]*checkpoint.Store
}

// contractPayload is the parsed result_contract event payload.
type contractPayload struct {
	Confidence          float64
	RequiresHumanReview bool
	TestsPassed         int
	TestsTotal          int
}

type Option func(*Server)

func WithExec(e *executor.Exec) Option { return func(s *Server) { s.exec = e } }
func WithIndex(html string) Option     { return func(s *Server) { s.index = html } }
func WithProbes(p []Probe) Option      { return func(s *Server) { s.probes = p } }

// WithAmeshAddr overrides the amesh-web address used to resolve mesh routes
// and list mesh models. Tests point it at a fake catalog.
func WithAmeshAddr(addr string) Option { return func(s *Server) { s.ameshAddr = addr } }
func WithHTTPClient(c *http.Client) Option {
	return func(s *Server) { s.client = c }
}
func WithHome(home string) Option { return func(s *Server) { s.home = home } }
func WithWorkDir(dir string) Option {
	return func(s *Server) { s.workDir = dir }
}

// workDirNow returns the current workspace directory (guarded by workMu so
// a runtime workspace switch cannot race a run spawn).
func (s *Server) workDirNow() string {
	s.workMu.RLock()
	defer s.workMu.RUnlock()
	return s.workDir
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
	// Backend selection: FLOBOT_BACKEND=taskclient routes runs over the
	// flocode task API (FLOCODE_TASK_URL, default loopback:7777); unset
	// or "executor" keeps the child-process executor. Any other value is
	// a config error and fails fast rather than silently degrading.
	switch b := os.Getenv("FLOBOT_BACKEND"); b {
	case "", "executor":
	case "taskclient":
		base := os.Getenv("FLOCODE_TASK_URL")
		if base == "" {
			base = "http://127.0.0.1:7777"
		}
		s.taskExec = &taskclient.Adapter{
			Client:   taskclient.New(base, nil),
			Fallback: &executor.Exec{},
		}
	default:
		log.Printf("flobot: invalid FLOBOT_BACKEND %q (want \"taskclient\" or \"executor\"); using executor", b)
	}
	s.hubOnce.Do(func() { s.hub = newHub() })
	s.snaps = newRunSnapshots()
	s.contracts = map[string]contractPayload{}
	// Supervision surfaces. The thread store is its own file
	// (~/.flobot/threads.json) so state.json's "never persists runs"
	// contract stays true.
	home := s.home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	ts := store.NewThreadStore()
	if home != "" {
		ts.SetPath(filepath.Join(home, ".flobot", "threads.json"))
	}
	if err := ts.Load(); err != nil {
		log.Printf("flobot: load threads: %v", err)
	}
	s.threads = threads.New(ts)
	s.queue = queue.New(ts)
	s.live = newLiveStore()
	s.cpRoots = map[string]*checkpoint.Store{}
	// Restore the roster from disk. A missing state file is not an error.
	if err := s.store.Load(); err != nil {
		log.Printf("flobot: load state: %v", err)
	}
	return s
}

// persist saves the roster after a successful mutation. A save failure is
// logged to stderr but never fails the HTTP handler.
func (s *Server) persist() {
	if s.store == nil {
		return
	}
	if err := s.store.Save(); err != nil {
		log.Printf("flobot: save state: %v", err)
	}
}

// sameOrigin blocks cross-origin browser requests (DNS-rebinding and
// hostile pages). Non-browser clients without an Origin header pass.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				http.Error(w, "cross-origin request denied", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.indexHTML)
	mux.HandleFunc("GET /styles.css", s.stylesCSS)
	mux.HandleFunc("GET /app.js", s.appJS)
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /api/v1/roster", s.roster)
	mux.HandleFunc("POST /api/v1/bots", s.createBot)
	mux.HandleFunc("PATCH /api/v1/bots/{id}", s.updateBot)
	mux.HandleFunc("DELETE /api/v1/bots/{id}", s.deleteBot)
	mux.HandleFunc("POST /api/v1/bots/{id}/remember", s.rememberBot)
	mux.HandleFunc("POST /api/v1/handoff", s.handoff)
	mux.HandleFunc("POST /api/v1/skills", s.createSkill)
	mux.HandleFunc("DELETE /api/v1/skills/{id}", s.deleteSkill)
	mux.HandleFunc("POST /api/v1/routines", s.createRoutine)
	mux.HandleFunc("PATCH /api/v1/routines/{id}", s.patchRoutine)
	mux.HandleFunc("POST /api/v1/routines/{id}/test-run", s.testRun)
	mux.HandleFunc("POST /api/v1/routines/{id}/enable", s.enableRoutine)
	mux.HandleFunc("POST /api/v1/routines/{id}/pause", s.pauseRoutine)
	mux.HandleFunc("DELETE /api/v1/routines/{id}", s.deleteRoutine)
	mux.HandleFunc("POST /api/v1/runs", s.createRun)
	mux.HandleFunc("GET /api/v1/runs", s.listRuns)
	mux.HandleFunc("GET /api/v1/runs/{id}", s.getRun)
	mux.HandleFunc("GET /api/v1/runs/{id}/events", s.runEvents)
	mux.HandleFunc("POST /api/v1/runs/{id}/stop", s.stopRun)
	mux.HandleFunc("GET /api/v1/health/models", s.healthModels)
	mux.HandleFunc("GET /api/v1/mesh/models", s.meshModels)
	mux.HandleFunc("GET /api/v1/hubs", s.hubModels)
	mux.HandleFunc("POST /api/v1/hubs/refresh", s.hubRefresh)
	mux.HandleFunc("GET /api/v1/hubs/doctor", s.hubDoctor)
	mux.HandleFunc("GET /api/v1/hubs/{name}", s.hubDetail)
	mux.HandleFunc("GET /api/v1/models", s.hubModels)
	mux.HandleFunc("POST /api/v1/models/{id}/start", s.modelStart)
	mux.HandleFunc("GET /api/v1/direct/models", s.directModels)
	mux.HandleFunc("GET /api/v1/local", s.localStatus)
	mux.HandleFunc("POST /api/v1/local/pin", s.localPin)
	mux.HandleFunc("GET /api/v1/workspace/root", s.workspaceRoot)
	mux.HandleFunc("GET /api/v1/workspace/roots", s.workspaceRoots)
	mux.HandleFunc("POST /api/v1/workspace/select", s.workspaceSelect)
	mux.HandleFunc("GET /api/v1/workspace/tree", s.workspaceTree)
	mux.HandleFunc("GET /api/v1/workspace/symbols", s.workspaceSymbols)
	mux.HandleFunc("GET /api/v1/workspace/file", s.workspaceFileGet)
	mux.HandleFunc("PUT /api/v1/workspace/file", s.workspaceFilePut)
	mux.HandleFunc("POST /api/v1/workspace/file", s.workspaceFilePost)
	mux.HandleFunc("DELETE /api/v1/workspace/file", s.workspaceFileDelete)
	mux.HandleFunc("POST /api/v1/workspace/rename", s.workspaceRename)
	mux.HandleFunc("GET /api/v1/workspace/descriptors", s.workspaceDescriptors)
	mux.HandleFunc("POST /api/v1/workspace/descriptors", s.workspaceDescriptorOpen)
	mux.HandleFunc("DELETE /api/v1/workspace/descriptors", s.workspaceDescriptorReleaseAll)
	mux.HandleFunc("GET /api/v1/workspace/descriptors/{id}", s.workspaceDescriptorGet)
	mux.HandleFunc("PUT /api/v1/workspace/descriptors/{id}", s.workspaceDescriptorPut)
	mux.HandleFunc("DELETE /api/v1/workspace/descriptors/{id}", s.workspaceDescriptorDelete)
	mux.HandleFunc("POST /api/v1/runs/{id}/diff/apply", s.applyDiff)
	mux.HandleFunc("POST /api/v1/runs/{id}/diff/reject", s.rejectDiff)
	mux.HandleFunc("GET /api/v1/runs/{id}/diff", s.runDiff)
	mux.HandleFunc("GET /api/v1/runs/{id}/diff/live", s.runDiffLive)
	mux.HandleFunc("POST /api/v1/runs/{id}/diff/merge", s.mergeThreadDiff)
	mux.HandleFunc("POST /api/v1/threads", s.createThread)
	mux.HandleFunc("GET /api/v1/threads", s.listThreads)
	mux.HandleFunc("GET /api/v1/threads/{id}", s.getThread)
	mux.HandleFunc("DELETE /api/v1/threads/{id}", s.deleteThread)
	mux.HandleFunc("GET /api/v1/queue", s.listQueue)
	mux.HandleFunc("POST /api/v1/queue/{id}/state", s.setQueueState)
	mux.HandleFunc("DELETE /api/v1/queue/{id}", s.resolveQueueEntry)
	mux.HandleFunc("GET /api/v1/checkpoints", s.listCheckpoints)
	mux.HandleFunc("POST /api/v1/checkpoints/{id}/restore", s.restoreCheckpoint)
	mux.HandleFunc("GET /api/stream", s.stream)
	return sameOrigin(mux)
}

func DefaultAddr() string { return defaultAddr }

// StartScheduler begins firing enabled "every <duration>" routines. Each
// fire is a fresh flocode child with the skill body prepended (cron = fresh
// agent). Non-scheduled triggers (manual, event names) are ignored here.
func (s *Server) StartScheduler(ctx context.Context) {
	if s.sched != nil {
		return
	}
	s.sched = schedule.New()
	s.sched.Start(ctx, s.fireables, s.fireRoutine)
}

// fireables lists enabled routines whose trigger schedules work — an
// "every <duration>" interval or a "cron <expr>" expression. Trigger and
// LastFire must be carried through or Due() can never match.
func (s *Server) fireables() []schedule.Fireable {
	var out []schedule.Fireable
	for _, rt := range s.store.ListRoutines(Actor) {
		if rt.Paused {
			continue
		}
		if _, _, ok := schedule.ParseTrigger(rt.Trigger); !ok {
			continue
		}
		f := schedule.Fireable{
			RoutineID: rt.ID,
			BotID:     rt.BotID,
			SkillID:   rt.SkillID,
			Trigger:   rt.Trigger,
		}
		if rt.LastRun != "" {
			if ts, err := time.Parse(time.RFC3339, rt.LastRun); err == nil {
				f.LastFire = ts
			}
		}
		out = append(out, f)
	}
	return out
}

// fireRoutine spawns a fresh ask-intent run for a scheduled routine. The
// skill body is prepended as the how-to. Errors are returned to the
// scheduler, which skips and retries next tick.
func (s *Server) fireRoutine(f schedule.Fired) error {
	rt, err := s.store.GetRoutine(f.RoutineID, Actor)
	if err != nil {
		return err
	}
	if rt.Paused {
		return nil
	}
	bot, err := s.store.GetBot(f.BotID, Actor)
	if err != nil {
		return fmt.Errorf("routine %s bot: %w", f.RoutineID, err)
	}
	// Re-fire guard: if the bot's previous routine run is still going
	// (e.g. hung on a slow route), skip this tick rather than piling up
	// concurrent runs for the same bot.
	if s.store.HasRunningRun(bot.ID) {
		return nil
	}
	sk, err := s.store.GetSkill(f.SkillID, Actor)
	if err != nil {
		return fmt.Errorf("routine %s skill: %w", f.RoutineID, err)
	}
	// Bound route resolution: a hung amesh-web must not stall the
	// scheduler tick forever (it runs on context.Background()).
	ctx, cancel := context.WithTimeout(context.Background(), amesh.FetchBudget)
	defer cancel()
	pin, cfgOverride, err := s.resolveRoute(ctx, bot.Route)
	if err != nil {
		return fmt.Errorf("routine %s route: %w", f.RoutineID, err)
	}
	prompt := "Scheduled routine fire. Follow the skill how-to.\n\n" + sk.Body
	run, err := s.store.CreateRun(Actor, bot.ID, prompt, "ask", false, false, "", pin, bot.Route)
	if err != nil {
		return fmt.Errorf("routine %s run: %w", f.RoutineID, err)
	}
	s.spawn(run, executor.Request{
		Prompt:         prompt,
		SkillBody:      sk.Body,
		Pin:            pin,
		Intent:         "ask",
		Route:          bot.Route,
		Dir:            s.workDirNow(),
		ConfigOverride: cfgOverride,
	})
	s.hub.publish(store.Event{Kind: "routine_fire", Text: rt.ID})
	// Record the fire on the routine: the store keeps LastRun/LastNote, not
	// the run itself, and the card reads them. LastRun also feeds
	// fireables().LastFire so a fired cron routine does not re-fire in the
	// same minute.
	if _, err := s.store.TestRun(rt.ID, rt.Owner, run.ID, "scheduled fire "+run.ID); err != nil {
		return fmt.Errorf("routine %s record fire: %w", rt.ID, err)
	}
	return nil
}

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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	s.StartScheduler(ctx)
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      0, // run streams are long-lived; per-request ctx bounds them
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		// Graceful drain: in-flight requests get 10s, then close.
		shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := srv.Shutdown(shCtx)
		// Shutdown returns when handlers finish, but detached work
		// (applyDiff's 2-min apply loop, running runs) outlives them.
		// Wait briefly for reviews to finish so a Ctrl-C mid-apply
		// does not kill the workspace half-reverted.
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			// TryLock: a held lock means a review is mid-flight.
			if s.revMu.TryLock() {
				idle := s.store == nil || !s.store.AnyRunningRun()
				s.revMu.Unlock()
				if idle {
					break
				}
			}
			time.Sleep(200 * time.Millisecond)
		}
		return err
	}
}

func (s *Server) indexHTML(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, s.index)
}

func (s *Server) stylesCSS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = io.WriteString(w, web.StylesCSS)
}

func (s *Server) appJS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = io.WriteString(w, web.AppJS)
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
		"routines":     routineViews(s.store.ListRoutines(Actor), time.Now()),
		"messages":     s.store.ListMessages(Actor),
		"runs":         s.runsWithSummaries(),
		"routes":       routes.DefaultRoutes(),
	})
}

// runsWithSummaries returns the in-memory runs with the persisted
// diffSummary/reviewState merged in (AC-2f: run history shows both). The
// summaries live in the RunStore (survives restarts); the in-memory Run
// struct carries no diff fields, so the merge is the single place that
// reconciles them for the roster and the runs list.
func (s *Server) runsWithSummaries() []store.Run {
	runs := s.store.ListRuns(Actor)
	rs := s.runStore()
	for i := range runs {
		sum, ok := rs.Get(runs[i].ID)
		if !ok {
			continue
		}
		runs[i].DiffSummary = sum.DiffSummary
		runs[i].ReviewState = sum.ReviewState
		runs[i].ReviewedAt = sum.ReviewedAt
		runs[i].HunksAccepted = sum.HunksAccepted
		runs[i].HunksTotal = sum.HunksTotal
		runs[i].JudgeScore = sum.JudgeScore
		runs[i].JudgeRationale = sum.JudgeRationale
	}
	return runs
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
		s.httpError(w, err)
		return
	}
	s.persist()
	writeJSON(w, bot)
}

// updateBot changes mutable bot fields (name, role, route); empty fields
// keep their current value.
func (s *Server) updateBot(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Role  string `json:"role"`
		Route string `json:"route"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	bot, err := s.store.UpdateBot(r.PathValue("id"), Actor, body.Name, body.Role, body.Route)
	if err != nil {
		s.httpError(w, err)
		return
	}
	s.persist()
	writeJSON(w, bot)
}

func (s *Server) deleteBot(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteBot(r.PathValue("id"), Actor); err != nil {
		s.httpError(w, err)
		return
	}
	s.persist()
	w.WriteHeader(http.StatusNoContent)
}

// rememberBot stores a short role note on a bot (Grok Bot-style teammate
// memory). The note is prompt text, never a secret.
func (s *Server) rememberBot(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Note string `json:"note"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	bot, err := s.store.Remember(r.PathValue("id"), Actor, body.Note)
	if err != nil {
		s.httpError(w, err)
		return
	}
	s.persist()
	writeJSON(w, bot)
}

func (s *Server) handoff(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
		Body string `json:"body"`
		// Run, when true, spawns a BOT_RUN for the recipient bot with the
		// handoff body as its prompt (Grok Bot-style: the handoff recipient
		// acts on the work item, not just receives it). The run uses the
		// recipient's route and intent ask unless the recipient bot's role
		// is a coder role.
		Run bool `json:"run"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	msg, err := s.store.Handoff(Actor, body.From, body.To, body.Body)
	if err != nil {
		s.httpError(w, err)
		return
	}
	s.persist()
	out := map[string]any{"message": msg}
	if body.Run {
		bot, err := s.store.GetBot(body.To, Actor)
		if err != nil {
			s.httpError(w, err)
			return
		}
		pin, cfgOverride, err := s.resolveRoute(r.Context(), bot.Route)
		if err != nil {
			s.httpError(w, err)
			return
		}
		prompt := "Handoff from " + body.From + ":\n\n" + body.Body
		intent := "ask"
		if strings.Contains(strings.ToLower(bot.Role), "cod") {
			intent = "code"
		}
		run, err := s.store.CreateRun(Actor, bot.ID, prompt, intent, false, false, "", pin, bot.Route)
		if err != nil {
			s.httpError(w, err)
			return
		}
		s.spawn(run, executor.Request{
			Prompt:         prompt,
			Pin:            pin,
			Intent:         intent,
			Route:          bot.Route,
			Dir:            s.workDirNow(),
			ConfigOverride: cfgOverride,
		})
		out["run"] = run
	}
	writeJSON(w, out)
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
		s.httpError(w, err)
		return
	}
	s.persist()
	writeJSON(w, sk)
}

func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSkill(r.PathValue("id"), Actor); err != nil {
		s.httpError(w, err)
		return
	}
	s.persist()
	w.WriteHeader(http.StatusNoContent)
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
	if err := validateTrigger(body.Trigger); err != nil {
		s.httpError(w, err)
		return
	}
	rt, err := s.store.CreateRoutine(Actor, body.BotID, body.SkillID, body.Trigger)
	if err != nil {
		s.httpError(w, err)
		return
	}
	s.persist()
	writeJSON(w, rt)
}

// validateTrigger rejects a schedule that announces itself as a cron or an
// interval but does not parse. Without this a typo'd cron ("cron 09 * *")
// is stored happily and then treated as manual: the routine never fires and
// nothing says why. A trigger that is neither ("manual", an event trigger)
// is allowed — it never fires from the ticker by design.
func validateTrigger(trigger string) error {
	t := strings.TrimSpace(trigger)
	switch lower := strings.ToLower(t); {
	case strings.HasPrefix(lower, "cron "):
		if _, err := schedule.ParseCron(strings.TrimSpace(t[len("cron "):])); err != nil {
			return fmt.Errorf("invalid cron schedule %q: %w", t, err)
		}
	case strings.HasPrefix(lower, "every "):
		if _, ok := schedule.ParseEvery(t); !ok {
			return fmt.Errorf("invalid interval schedule %q: want a duration like \"every 5m\"", t)
		}
	}
	return nil
}

func (s *Server) testRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rt, err := s.store.GetRoutine(id, Actor)
	if err != nil {
		s.httpError(w, err)
		return
	}
	bot, err := s.store.GetBot(rt.BotID, Actor)
	if err != nil {
		s.httpError(w, err)
		return
	}
	sk, err := s.store.GetSkill(rt.SkillID, Actor)
	if err != nil {
		s.httpError(w, err)
		return
	}
	pin, cfgOverride, err := s.resolveRoute(r.Context(), bot.Route)
	if err != nil {
		s.httpError(w, err)
		return
	}
	prompt := "Test-run the following skill. Ask/read only; do not mutate the workspace.\n\n" + sk.Body
	run, err := s.store.CreateRun(Actor, bot.ID, prompt, "ask", false, false, "", pin, bot.Route)
	if err != nil {
		s.httpError(w, err)
		return
	}
	rt, err = s.store.TestRun(rt.ID, Actor, run.ID, "")
	if err != nil {
		s.httpError(w, err)
		return
	}
	s.persist()
	s.spawn(run, executor.Request{
		Prompt:         prompt,
		SkillBody:      sk.Body,
		Pin:            pin,
		Intent:         "ask",
		Yolo:           false,
		Verify:         false,
		Route:          bot.Route,
		Timeout:        executor.AskTimeout,
		Dir:            s.workDirNow(),
		ConfigOverride: cfgOverride,
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
	id := r.PathValue("id")
	cur, err := s.store.GetRoutine(id, Actor)
	if err != nil {
		s.httpError(w, err)
		return
	}
	// Arming a routine whose schedule does not parse would arm a fire that
	// can never happen; refuse with the reason instead.
	if err := validateTrigger(cur.Trigger); err != nil {
		s.httpError(w, err)
		return
	}
	rt, err := s.store.EnableRoutine(id, Actor)
	if err != nil {
		s.httpError(w, err)
		return
	}
	s.persist()
	writeJSON(w, rt)
}

// resolveRoute maps a bot route to a flocode pin. Mesh routes resolve
// against the live amesh catalog and also return a generated config
// override (no secrets) for the child.
func (s *Server) pauseRoutine(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paused bool `json:"paused"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	rt, err := s.store.PauseRoutine(r.PathValue("id"), Actor, body.Paused)
	if err != nil {
		s.httpError(w, err)
		return
	}
	s.persist()
	writeJSON(w, rt)
}

func (s *Server) deleteRoutine(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteRoutine(r.PathValue("id"), Actor); err != nil {
		s.httpError(w, err)
		return
	}
	s.persist()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) resolveRoute(ctx context.Context, route string) (pin string, cfgOverride string, err error) {
	if amesh.IsMeshRoute(route) {
		mp, err := routes.ResolveMeshPin(ctx, route, s.ameshAddr)
		if err != nil {
			return "", "", err
		}
		return mp.Pin, mp.Config, nil
	}
	if routes.IsDirectRoute(route) {
		mid := routes.ModelIDFromDirectRoute(route)
		if mid == "" {
			return "", "", fmt.Errorf("empty direct route %q", route)
		}
		return amesh.PinName(mid), routes.DirectConfigTOML(mid), nil
	}
	pin, err = routes.ResolvePin(route)
	if err != nil {
		return "", "", err
	}
	return pin, "", nil
}

// meshModels lists the ready amesh mesh models as flobot routes. An amesh-web
// that is down or a hub that is unreachable is a rendered state, not a 500.
func (s *Server) meshModels(w http.ResponseWriter, r *http.Request) {
	models, err := (&amesh.Client{Addr: s.ameshAddr}).Models(r.Context())
	if err != nil {
		// Contract: ok=false with 200 and an empty model list. The UI
		// renders "amesh unreachable" from the reason; a 5xx would make
		// api() throw and drop the whole roster panel.
		writeJSON(w, map[string]any{
			"schema": "flobot.mesh_models.v1",
			"models": []amesh.Model{},
			"ok":     false,
			"reason": err.Error(),
		})
		return
	}
	ready := amesh.Ready(models)
	rows := make([]map[string]any, 0, len(ready))
	for _, m := range ready {
		rows = append(rows, map[string]any{
			"route":          amesh.RouteID(m.ID),
			"model":          m.ID,
			"display":        m.Display,
			"node_id":        m.NodeID,
			"context_tokens": m.ContextTokens,
			"status_text":    m.StatusText,
		})
	}
	writeJSON(w, map[string]any{
		"schema": "flobot.mesh_models.v1",
		"ok":     true,
		"models": rows,
	})
}

// directModels lists the models available on the Meta model API, fetched
// server-side so the API key never reaches the browser. Used by the route
// wizard when amesh is unreachable or the user wants to force a direct route.
func (s *Server) directModels(w http.ResponseWriter, r *http.Request) {
	key, err := executor.ReadModelAPIKey("")
	if err != nil {
		writeJSON(w, map[string]any{
			"schema": "flobot.direct_models.v1",
			"ok":     false,
			"reason": "no Meta API key on this Mac (~/.accounts/meta.ai): " + err.Error(),
			"models": []map[string]any{},
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), amesh.FetchBudget)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", routes.DirectBaseURL+"/models", nil)
	if err != nil {
		s.httpError(w, fmt.Errorf("build models request: %w", err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, map[string]any{
			"schema": "flobot.direct_models.v1",
			"ok":     false,
			"reason": "Meta model API unreachable: " + err.Error(),
			"models": []map[string]any{},
		})
		return
	}
	defer resp.Body.Close()
	var list struct {
		Data []struct {
			ID      string `json:"id"`
			Display string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil || resp.StatusCode != 200 {
		writeJSON(w, map[string]any{
			"schema": "flobot.direct_models.v1",
			"ok":     false,
			"reason": fmt.Sprintf("Meta model API error (status %d)", resp.StatusCode),
			"models": []map[string]any{},
		})
		return
	}
	rows := make([]map[string]any, 0, len(list.Data))
	for _, m := range list.Data {
		rows = append(rows, map[string]any{
			"route":   "direct/" + m.ID,
			"model":   m.ID,
			"display": m.Display,
		})
	}
	writeJSON(w, map[string]any{
		"schema": "flobot.direct_models.v1",
		"ok":     true,
		"models": rows,
	})
}

// localStatus reports whether the "local" route is pinned and, if so, to
// which flocode pin. The pin name is not a secret.
func (s *Server) localStatus(w http.ResponseWriter, _ *http.Request) {
	pin := routes.LocalPin()
	writeJSON(w, map[string]any{
		"schema": "flobot.local.v1",
		"route":  "local",
		"pinned": pin != "",
		"pin":    pin,
	})
}

// localPin pins the "local" route to a flocode model pin, or unpins it with
// an empty pin. This is how the route wizard makes the local MLX route
// usable. The pin name is not a secret; it never carries a key.
func (s *Server) localPin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pin string `json:"pin"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	routes.SetLocalPin(body.Pin)
	s.persist()
	pin := routes.LocalPin()
	writeJSON(w, map[string]any{
		"schema": "flobot.local.v1",
		"route":  "local",
		"pinned": pin != "",
		"pin":    pin,
	})
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
		s.httpError(w, err)
		return
	}
	pin, cfgOverride, err := s.resolveRoute(r.Context(), bot.Route)
	if err != nil {
		s.httpError(w, err)
		return
	}
	intent := strings.TrimSpace(body.Intent)
	if intent == "" {
		intent = "code"
	}
	// Prepend the bot's memory note so a teammate remembers its role
	// across runs (Grok Bot-style). Memory is prompt text, never a secret.
	prompt := body.Prompt
	if bot.Memory != "" {
		prompt = "Teammate memory (you said to remember this):\n" + bot.Memory + "\n\n" + prompt
	}
	run, err := s.store.CreateRun(Actor, bot.ID, prompt, intent, body.Yolo, body.Verify, body.Worktree, pin, bot.Route)
	if err != nil {
		s.httpError(w, err)
		return
	}
	s.spawn(run, executor.Request{
		Prompt:         prompt,
		Pin:            pin,
		Intent:         intent,
		Yolo:           body.Yolo,
		Verify:         body.Verify,
		Worktree:       body.Worktree,
		Route:          bot.Route,
		Dir:            s.workDirNow(),
		ConfigOverride: cfgOverride,
	})
	writeJSON(w, run)
}

func (s *Server) listRuns(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"runs": s.runsWithSummaries()})
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetRun(r.PathValue("id"), Actor)
	if err != nil {
		s.httpError(w, err)
		return
	}
	run.Events = nil
	writeJSON(w, run)
}

func (s *Server) runEvents(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetRun(r.PathValue("id"), Actor)
	if err != nil {
		s.httpError(w, err)
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
	// A run started mid-apply/reject would write the workspace while
	// applyDiff rebuilds files from pre-run content, silently
	// clobbering the run's edits. Serialize against reviews: wait
	// for an in-flight review to finish before the run starts.
	s.revMu.Lock()
	s.revMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelsMu.Lock()
	if s.cancels == nil {
		s.cancels = make(map[string]context.CancelFunc)
	}
	s.cancels[run.ID] = cancel
	s.cancelsMu.Unlock()
	// Live diff: poll this run's tree so the review pane fills while the
	// agent is still working. The poller stops with the run's context.
	s.startLiveDiff(ctx, run.ID)
	// Pre-run git snapshot (single choke point: createRun, handoff,
	// testRun, fireRoutine). Fail-closed: snapshot errors never block
	// the run, they only disable diff review for it. Runs in its own
	// goroutine so the HTTP create-run request does not block on
	// `git add -A` of a large worktree; the run itself waits on
	// snapDone so the pre tree can never capture the run's own writes.
	snapDone := make(chan struct{})
	go func() {
		defer close(snapDone)
		s.revMu.Lock()
		s.revMu.Unlock()
		s.capturePreRun(run.ID)
	}()
	go func() {
		defer func() {
			s.cancelsMu.Lock()
			delete(s.cancels, run.ID)
			delete(s.stopped, run.ID)
			s.cancelsMu.Unlock()
			cancel()
			if rec := recover(); rec != nil {
				_ = s.store.FinishRun(run.ID, Actor, "error", "internal error")
				// Publish so stream clients learn the run died; without this
				// the UI waits forever on a panicked run.
				s.hub.publish(store.Event{Kind: "run_status", Text: "error"})
				// The normal path's capturePostRun is skipped on panic;
				// still record a summary and evict per-run state so a
				// panicked run cannot leak snaps/contracts entries.
				s.snaps.evict(run.ID)
				s.contractsMu.Lock()
				delete(s.contracts, run.ID)
				s.contractsMu.Unlock()
			}
		}()
		<-snapDone
		res := s.runBackend(ctx, req, func(ev executor.Event) {
			_ = s.store.AppendEvent(run.ID, Actor, store.Event{
				Kind:    ev.Kind,
				TurnID:  ev.TurnID,
				Text:    ev.Text,
				Tool:    ev.Tool,
				IsError: ev.IsError,
			})
			s.hub.publish(store.Event{
				Kind:    ev.Kind,
				TurnID:  ev.TurnID,
				Text:    ev.Text,
				Tool:    ev.Tool,
				IsError: ev.IsError,
			})
			// Capture the result contract (taskclient backend) for the
			// run summary: confidence, tests, requires_human_review.
			if ev.Kind == "result_contract" {
				s.noteResultContract(run.ID, ev.Text)
			}
		})
		status := "done"
		errText := ""
		if res.Err != nil {
			status = "error"
			errText = res.Err.Error()
			// A bare "exit status 1" hides the cause. Map flocode's known
			// fatal failure modes (unreadable events, process aborts) to a
			// one-line hint, appending the scrubbed stderr diagnostic tail so
			// the run detail shows why, not just that.
			if hint := runFailureHint(errText, res.Stderr); hint != "" {
				errText = errText + " — " + hint
			}
		}
		if s.wasStopped(run.ID) {
			// A user-initiated stop is not an error; surface it as such.
			status = "stopped"
			errText = ""
		}
		_ = s.store.FinishRun(run.ID, Actor, status, errText)
		// Post-run snapshot + diff + run summary (fail-closed).
		s.capturePostRun(run.ID, run.BotID, status)
		// Judge loop: score the finished run against its prompt
		// (advisory, fail-open, never blocks the run's completion).
		s.judgeRun(run, req)
		s.hub.publish(store.Event{Kind: "run_status", Text: status})
	}()
}

// stopRun cancels a running run's executor context, killing its process
// group. Idempotent: stopping a finished or unknown run is a no-op 200.
func (s *Server) stopRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.cancelsMu.Lock()
	cancel, ok := s.cancels[id]
	if ok {
		if s.stopped == nil {
			s.stopped = make(map[string]bool)
		}
		s.stopped[id] = true
	}
	s.cancelsMu.Unlock()
	if ok {
		cancel()
	}
	writeJSON(w, map[string]any{"id": id, "stopped": ok})
}

// wasStopped reports whether id was cancelled by POST /runs/{id}/stop.
func (s *Server) wasStopped(id string) bool {
	s.cancelsMu.Lock()
	defer s.cancelsMu.Unlock()
	return s.stopped[id]
}

// stream is the NDJSON event feed. It replays the events of the run given
// by ?run= (if any), then forwards live executor events until the client
// disconnects. A disconnect or reconnect never affects the server or other
// clients.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Flush the headers now so the client's request returns immediately,
	// even before the first event arrives.
	fl.Flush()

	// Subscribe before replaying so no event can slip between the replay
	// snapshot and the subscription: anything appended after the snapshot
	// is re-read from the store below and emitted as a tail.
	ch, cancel := s.hub.subscribe()
	defer cancel()

	// drain discards anything already queued on ch so the tail re-read
	// below is the sole source for pre-live events (no duplicates).
	drain := func() {
		for {
			select {
			case <-ch:
			default:
				return
			}
		}
	}

	// Replay the tail of a specific run so a reconnecting client catches up.
	// ?since=N skips the first N events (the client already has them),
	// letting a dropped connection resume instead of re-rendering.
	if id := strings.TrimSpace(r.URL.Query().Get("run")); id != "" {
		since := 0
		if v := strings.TrimSpace(r.URL.Query().Get("since")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				since = n
			}
		}
		if run, err := s.store.GetRun(id, Actor); err == nil {
			if since > len(run.Events) {
				since = len(run.Events)
			}
			for _, ev := range run.Events[since:] {
				if !writeNDJSON(w, ev) {
					return
				}
			}
			fl.Flush()
			// Events appended between the snapshot above and the drain are
			// re-read from the store and emitted as a tail; the live channel
			// covers everything after the drain.
			drain()
			if run2, err := s.store.GetRun(id, Actor); err == nil {
				for _, ev := range run2.Events[len(run.Events):] {
					if !writeNDJSON(w, ev) {
						return
					}
				}
				fl.Flush()
			}
		}
	}

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := enc.Encode(ev); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

func writeNDJSON(w http.ResponseWriter, ev store.Event) bool {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(ev) == nil
}

// hub fans executor events out to every connected /api/stream client.
// Each subscriber owns a buffered channel; a slow or gone client is
// dropped without blocking the publisher or the other clients.
type hub struct {
	mu   sync.Mutex
	next uint64
	subs map[uint64]chan store.Event
}

func newHub() *hub {
	return &hub{subs: make(map[uint64]chan store.Event)}
}

func (h *hub) subscribe() (<-chan store.Event, func()) {
	h.mu.Lock()
	h.next++
	id := h.next
	ch := make(chan store.Event, 64)
	h.subs[id] = ch
	h.mu.Unlock()
	cancel := func() {
		h.mu.Lock()
		if c, ok := h.subs[id]; ok {
			delete(h.subs, id)
			close(c)
		}
		h.mu.Unlock()
	}
	return ch, cancel
}

func (h *hub) publish(ev store.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, ch := range h.subs {
		select {
		case ch <- ev:
		default:
			// Slow client: drop the event and the client rather than
			// block the publisher.
			delete(h.subs, id)
			close(ch)
		}
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, limit int64, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, limit))
	if err := dec.Decode(dst); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return false
	}
	// Reject trailing garbage after the first JSON value.
	if dec.More() {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return false
	}
	return true
}

func (s *Server) httpError(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	switch {
	case errors.Is(err, store.ErrDenied):
		code = http.StatusForbidden
	case errors.Is(err, store.ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, amesh.ErrUnreachable):
		// amesh-web down or timing out: our dependency failed, not the request.
		code = http.StatusBadGateway
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		code = http.StatusGatewayTimeout
	case errors.Is(err, routes.ErrLocalUnpinned):
		code = http.StatusBadRequest
	case errors.Is(err, workspace.ErrProtected):
		code = http.StatusForbidden
	case errors.Is(err, workspace.ErrNotInWorkspace):
		code = http.StatusBadRequest
	case errors.Is(err, workspace.ErrTooLarge):
		code = http.StatusRequestEntityTooLarge
	case errors.Is(err, workspace.ErrExists):
		code = http.StatusConflict
	case errors.Is(err, workspace.ErrShaMismatch):
		code = http.StatusConflict
	}

	// Scrub absolute paths from the message: git/os errors embed the
	// operator's home layout and workspace root, which must not leak
	// if the surface is ever bound beyond localhost.
	msg := s.scrubPaths(err.Error())
	http.Error(w, msg, code)
}

// scrubPaths replaces absolute path prefixes in an error message with
// their last two segments. It is best-effort: it targets the known
// leak vectors (workspace root, home dir, temp dirs) rather than every
// possible path.
func (s *Server) scrubPaths(msg string) string {
	for _, root := range []string{s.home, s.workDirNow()} {
		if root == "" {
			continue
		}
		for strings.Contains(msg, root) {
			msg = strings.Replace(msg, root, filepath.Base(root), 1)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		msg = strings.ReplaceAll(msg, home, "~")
	}
	if tmp := os.TempDir(); tmp != "" && tmp != "/" {
		msg = strings.ReplaceAll(msg, tmp, "$TMPDIR")
	}
	return msg
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
