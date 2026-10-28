// Package store is the in-memory roster: bots, skills, routines, messages, runs.
// API keys never live on these types. Lost on process restart.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/florianleibert/flobot/internal/routes"
)

// ActorLocal is the standalone web actor (no pairing session).
const ActorLocal = "local"

// ErrDenied is returned when the actor does not own the record.
var ErrDenied = errors.New("flobot denied")

// ErrNotFound is returned when the record does not exist at all. Ownership
// mismatches stay ErrDenied so callers cannot enumerate other actors' ids.
var ErrNotFound = errors.New("flobot not found")

const (
	textCap    = 8 << 10
	maxEvents  = 2000
	maxRuns    = 200 // finished runs kept; oldest evicted
	maxMsgs    = 500 // messages kept; oldest evicted
	idRandSize = 8
)

// Bot is a named teammate. Memory is a short role note.
type Bot struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Route     string `json:"route"`
	Memory    string `json:"memory,omitempty"`
	Owner     string `json:"-"`
	CreatedAt string `json:"created_at"`
}

// Skill is a reviewable how-to. Never live from a raw recording.
type Skill struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	Version int    `json:"version"`
	Owner   string `json:"-"`
}

// Routine binds a skill to a bot and a trigger. Test-run before enable.
type Routine struct {
	ID       string `json:"id"`
	BotID    string `json:"bot_id"`
	SkillID  string `json:"skill_id"`
	Trigger  string `json:"trigger"`
	Paused   bool   `json:"paused"`
	LastRun  string `json:"last_run,omitempty"`
	LastNote string `json:"last_note,omitempty"`
	Owner    string `json:"-"`
}

// Message is a bus item. OwnerBot encodes single-owner-per-stage.
type Message struct {
	ID        string `json:"id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Group     string `json:"group,omitempty"`
	Body      string `json:"body"`
	OwnerBot  string `json:"owner_bot"`
	CreatedAt string `json:"created_at"`
}

// Event is one streaming-json line. Unknown fields are never stored.
type Event struct {
	Kind    string `json:"kind"`
	TurnID  uint64 `json:"turn_id,omitempty"`
	Text    string `json:"text,omitempty"`
	Tool    string `json:"tool,omitempty"`
	IsError *bool  `json:"is_error,omitempty"`
}

// Run is one flocode child. Events stay off the default JSON (json:"-").
type Run struct {
	ID         string  `json:"id"`
	BotID      string  `json:"bot_id,omitempty"`
	Prompt     string  `json:"prompt"`
	Intent     string  `json:"intent"`
	Yolo       bool    `json:"yolo"`
	Verify     bool    `json:"verify"`
	Worktree   string  `json:"worktree,omitempty"`
	Pin        string  `json:"pin,omitempty"`
	Route      string  `json:"route,omitempty"`
	Status     string  `json:"status"`
	Err        string  `json:"error,omitempty"`
	EventCount int     `json:"event_count"`
	CreatedAt  string  `json:"created_at"`
	EndedAt    string  `json:"ended_at,omitempty"`
	Owner      string  `json:"-"`
	Events     []Event `json:"-"`
	// Code Studio AC-2f: merged from the persisted RunStore by
	// Server.runsWithSummaries so run history shows the diff and review
	// state without duplicating the summary fields on the in-memory Run.
	DiffSummary *DiffStat `json:"diffSummary,omitempty"`
	ReviewState string    `json:"reviewState,omitempty"` // pending|accepted|rejected|partial
	// Review metrics (PRD §11), merged from RunStore like the above.
	ReviewedAt    string `json:"reviewedAt,omitempty"`
	HunksAccepted int    `json:"hunksAccepted,omitempty"`
	HunksTotal    int    `json:"hunksTotal,omitempty"`
	// Judge loop, merged from RunStore like the above.
	JudgeScore     float64 `json:"judgeScore,omitempty"`
	JudgeRationale string  `json:"judgeRationale,omitempty"`
}

// Store is the in-memory authority. Nothing persists across restart.
type Store struct {
	*State // embedded: State's methods (Load, Save, ListBots, ...) are promoted

	mu   sync.Mutex
	runs map[string]Run
}

func New() *Store {
	st := NewState()
	// Seed the pinned roster so a fresh store behaves like a loaded one.
	st.applyDefaults()
	return &Store{
		State: st,
		runs:  make(map[string]Run),
	}
}

// SetPersistPath overrides the state file location (tests). It is a
// compatibility alias for State.SetPath.
func (s *Store) SetPersistPath(p string) {
	s.State.SetPath(p)
}

// Save persists the roster. It is a compatibility alias for State.Save.
// A failed save is never fatal, but it is logged to stderr so a broken
// state file is not silently ignored.
func (s *Store) Save() error {
	// Snapshot under the Store lock so concurrent mutations (e.g. two
	// handoffs) cannot race with State.Save reading s.messages.
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.State.Save()
	if err != nil {
		fmt.Fprintf(os.Stderr, "flobot: state save failed: %v\n", err)
	}
	return err
}

func (s *Store) CreateBot(owner, name, role, route string) (Bot, error) {
	if owner == "" {
		return Bot{}, fmt.Errorf("owner required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Bot{}, fmt.Errorf("name required")
	}
	if err := routes.Validate(route); err != nil {
		return Bot{}, err
	}
	if strings.TrimSpace(role) == "" {
		role = "specialist"
	}
	if strings.TrimSpace(route) == "" {
		route = "glm"
	}
	bot := Bot{
		ID:        newID("bot"),
		Name:      name,
		Role:      strings.TrimSpace(role),
		Route:     strings.TrimSpace(route),
		Owner:     owner,
		CreatedAt: nowRFC3339(),
	}
	s.mu.Lock()
	s.bots[bot.ID] = bot
	s.mu.Unlock()
	s.Save()
	return bot, nil
}

// DeleteBot removes a bot. Routines referencing it are removed too (a
// routine without its bot can never fire); its runs and messages are kept.
func (s *Store) DeleteBot(id, actor string) error {
	if _, err := s.ownedBot(id, actor); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.bots, id)
	for rid, rt := range s.routines {
		if rt.BotID == id {
			delete(s.routines, rid)
		}
	}
	s.mu.Unlock()
	s.Save()
	return nil
}

// UpdateBot changes mutable fields (name, role, route). Empty fields keep
// their current value.
func (s *Store) UpdateBot(id, actor, name, role, route string) (Bot, error) {
	bot, err := s.ownedBot(id, actor)
	if err != nil {
		return Bot{}, err
	}
	if name = strings.TrimSpace(name); name != "" {
		bot.Name = name
	}
	if role = strings.TrimSpace(role); role != "" {
		bot.Role = role
	}
	if route = strings.TrimSpace(route); route != "" {
		if err := routes.Validate(route); err != nil {
			return Bot{}, err
		}
		bot.Route = route
	}
	s.mu.Lock()
	s.bots[id] = bot
	s.mu.Unlock()
	s.Save()
	return bot, nil
}

func (s *Store) GetBot(id, actor string) (Bot, error) {
	return s.ownedBot(id, actor)
}

func (s *Store) ownedBot(id, actor string) (Bot, error) {
	if id == "" || actor == "" {
		return Bot{}, ErrDenied
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bot, ok := s.bots[id]
	if !ok {
		return Bot{}, ErrNotFound
	}
	if bot.Owner != actor {
		return Bot{}, ErrDenied
	}
	return bot, nil
}

func (s *Store) ListBots(actor string) []Bot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Bot, 0, len(s.bots))
	for _, bot := range s.bots {
		if bot.Owner == actor {
			out = append(out, bot)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out
}

func (s *Store) Remember(id, actor, note string) (Bot, error) {
	bot, err := s.ownedBot(id, actor)
	if err != nil {
		return Bot{}, err
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return Bot{}, fmt.Errorf("memory note required")
	}
	bot.Memory = clip(note)
	s.mu.Lock()
	s.bots[id] = bot
	s.mu.Unlock()
	return bot, nil
}

func (s *Store) CreateSkill(owner, name, body string) (Skill, error) {
	if owner == "" {
		return Skill{}, fmt.Errorf("owner required")
	}
	name = strings.TrimSpace(name)
	body = strings.TrimSpace(body)
	if name == "" || body == "" {
		return Skill{}, fmt.Errorf("name and body required")
	}
	sk := Skill{ID: newID("skill"), Name: name, Body: clip(body), Version: 1, Owner: owner}
	s.mu.Lock()
	s.skills[sk.ID] = sk
	s.mu.Unlock()
	s.Save()
	return sk, nil
}

// DeleteSkill removes a skill and any routines that reference it.
func (s *Store) DeleteSkill(id, actor string) error {
	s.mu.Lock()
	sk, ok := s.skills[id]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	if sk.Owner != actor {
		s.mu.Unlock()
		return ErrDenied
	}
	delete(s.skills, id)
	for rid, rt := range s.routines {
		if rt.SkillID == id {
			delete(s.routines, rid)
		}
	}
	s.mu.Unlock()
	s.Save()
	return nil
}

func (s *Store) GetSkill(id, actor string) (Skill, error) {
	if id == "" || actor == "" {
		return Skill{}, ErrDenied
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sk, ok := s.skills[id]
	if !ok {
		return Skill{}, ErrNotFound
	}
	if sk.Owner != actor {
		return Skill{}, ErrDenied
	}
	return sk, nil
}

func (s *Store) ListSkills(actor string) []Skill {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Skill, 0, len(s.skills))
	for _, sk := range s.skills {
		if sk.Owner == actor {
			out = append(out, sk)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Store) CreateRoutine(owner, botID, skillID, trigger string) (Routine, error) {
	if _, err := s.ownedBot(botID, owner); err != nil {
		return Routine{}, err
	}
	s.mu.Lock()
	sk, ok := s.skills[skillID]
	s.mu.Unlock()
	if !ok || sk.Owner != owner {
		return Routine{}, fmt.Errorf("unknown skill")
	}
	trigger = strings.TrimSpace(trigger)
	if trigger == "" {
		return Routine{}, fmt.Errorf("trigger required")
	}
	rt := Routine{
		ID:      newID("rtn"),
		BotID:   botID,
		SkillID: skillID,
		Trigger: trigger,
		Paused:  true,
		Owner:   owner,
	}
	s.mu.Lock()
	s.routines[rt.ID] = rt
	s.mu.Unlock()
	s.Save()
	return rt, nil
}

// UpdateRoutine changes a routine's trigger and/or paused flag. A nil
// field is left unchanged. The trigger is validated by the caller before
// it gets here — the store only persists what it is given — so a routine's
// trigger can be corrected without deleting and recreating it.
func (s *Store) UpdateRoutine(id, actor string, trigger *string, paused *bool) (Routine, error) {
	s.mu.Lock()
	rt, ok := s.routines[id]
	if !ok {
		s.mu.Unlock()
		return Routine{}, ErrNotFound
	}
	if rt.Owner != actor {
		s.mu.Unlock()
		return Routine{}, ErrDenied
	}
	if trigger != nil {
		t := strings.TrimSpace(*trigger)
		if t == "" {
			s.mu.Unlock()
			return Routine{}, fmt.Errorf("trigger required")
		}
		rt.Trigger = t
	}
	if paused != nil {
		rt.Paused = *paused
	}
	s.routines[id] = rt
	s.mu.Unlock()
	s.Save()
	return rt, nil
}

// DeleteRoutine removes a routine.
func (s *Store) DeleteRoutine(id, actor string) error {
	s.mu.Lock()
	rt, ok := s.routines[id]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	if rt.Owner != actor {
		s.mu.Unlock()
		return ErrDenied
	}
	delete(s.routines, id)
	s.mu.Unlock()
	s.Save()
	return nil
}

// PauseRoutine toggles Paused. Unpausing requires a prior test-run, same
// as EnableRoutine.
func (s *Store) PauseRoutine(id, actor string, paused bool) (Routine, error) {
	s.mu.Lock()
	rt, ok := s.routines[id]
	if !ok || rt.Owner != actor {
		s.mu.Unlock()
		return Routine{}, ErrDenied
	}
	if !paused && rt.LastRun == "" {
		s.mu.Unlock()
		return Routine{}, fmt.Errorf("test-run required before enable")
	}
	rt.Paused = paused
	s.routines[id] = rt
	s.mu.Unlock()
	s.Save()
	return rt, nil
}

func (s *Store) GetRoutine(id, actor string) (Routine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, ok := s.routines[id]
	if !ok {
		return Routine{}, ErrNotFound
	}
	if rt.Owner != actor {
		return Routine{}, ErrDenied
	}
	return rt, nil
}

func (s *Store) ListRoutines(actor string) []Routine {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Routine, 0, len(s.routines))
	for _, rt := range s.routines {
		if rt.Owner == actor {
			out = append(out, rt)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// TestRun records LastRun and LastNote (including run id). The store does
// not exec; the server calls the executor then records here.
func (s *Store) TestRun(id, actor, runID, note string) (Routine, error) {
	s.mu.Lock()
	rt, ok := s.routines[id]
	if !ok || rt.Owner != actor {
		s.mu.Unlock()
		return Routine{}, ErrDenied
	}
	rt.LastRun = nowRFC3339()
	note = strings.TrimSpace(note)
	if note == "" {
		if runID != "" {
			note = "test-run " + runID
		} else {
			note = "test-run"
		}
	}
	rt.LastNote = clip(note)
	s.routines[id] = rt
	s.mu.Unlock()
	s.Save()
	return rt, nil
}

func (s *Store) EnableRoutine(id, actor string) (Routine, error) {
	s.mu.Lock()
	rt, ok := s.routines[id]
	if !ok || rt.Owner != actor {
		s.mu.Unlock()
		return Routine{}, ErrDenied
	}
	if rt.LastRun == "" {
		s.mu.Unlock()
		return Routine{}, fmt.Errorf("test-run required before enable")
	}
	rt.Paused = false
	s.routines[id] = rt
	s.mu.Unlock()
	s.Save()
	return rt, nil
}

// appendMessage validates ownership and appends the final message exactly
// once under a single lock. OwnerBot is decided before the append: never
// rewrite a bus item by index after the lock is released.
func (s *Store) appendMessage(owner, from, to, group, body, ownerBot string) (Message, error) {
	if _, err := s.ownedBot(from, owner); err != nil {
		return Message{}, err
	}
	if to != "@all" {
		if _, err := s.ownedBot(to, owner); err != nil {
			return Message{}, err
		}
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Message{}, fmt.Errorf("body required")
	}
	msg := Message{
		ID:        newID("msg"),
		From:      from,
		To:        to,
		Group:     strings.TrimSpace(group),
		Body:      clip(body),
		OwnerBot:  ownerBot,
		CreatedAt: nowRFC3339(),
	}
	s.mu.Lock()
	s.messages = append(s.messages, msg)
	if len(s.messages) > maxMsgs {
		s.messages = append(s.messages[:0], s.messages[len(s.messages)-maxMsgs:]...)
	}
	s.mu.Unlock()
	s.Save()
	return msg, nil
}

func (s *Store) Message(owner, from, to, group, body string) (Message, error) {
	ownerBot := to
	if to == "@all" {
		ownerBot = from
	}
	return s.appendMessage(owner, from, to, group, body, ownerBot)
}

// Handoff moves ownership of a work item to a named bot. "@all" is not a
// bot, so a broadcast handoff would leave the item with no single owner.
func (s *Store) Handoff(owner, from, to, body string) (Message, error) {
	if to == "@all" {
		return Message{}, fmt.Errorf("handoff requires a named bot")
	}
	return s.appendMessage(owner, from, to, "", body, to)
}

func (s *Store) ListMessages(actor string) []Message {
	bots := map[string]bool{}
	for _, bot := range s.ListBots(actor) {
		bots[bot.ID] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Message, 0, len(s.messages))
	for _, msg := range s.messages {
		if bots[msg.From] || bots[msg.To] || bots[msg.OwnerBot] {
			out = append(out, msg)
		}
	}
	return out
}

func (s *Store) CreateRun(owner, botID, prompt, intent string, yolo, verify bool, worktree, pin, route string) (Run, error) {
	if owner == "" {
		return Run{}, fmt.Errorf("owner required")
	}
	if botID != "" {
		if _, err := s.ownedBot(botID, owner); err != nil {
			return Run{}, err
		}
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Run{}, fmt.Errorf("prompt required")
	}
	intent = strings.TrimSpace(intent)
	if intent == "" {
		intent = "code"
	}
	switch intent {
	case "ask", "plan", "code":
	default:
		return Run{}, fmt.Errorf("unknown intent %q", intent)
	}
	run := Run{
		ID:        newID("run"),
		BotID:     botID,
		Prompt:    clip(prompt),
		Intent:    intent,
		Yolo:      yolo,
		Verify:    verify,
		Worktree:  strings.TrimSpace(worktree),
		Pin:       strings.TrimSpace(pin),
		Route:     strings.TrimSpace(route),
		Status:    "running",
		CreatedAt: nowRFC3339(),
		Owner:     owner,
	}
	s.mu.Lock()
	s.runs[run.ID] = run
	s.mu.Unlock()
	return run, nil
}

func (s *Store) AppendEvent(id, actor string, ev Event) error {
	if id == "" || actor == "" {
		return ErrDenied
	}
	ev.Kind = strings.TrimSpace(ev.Kind)
	if ev.Kind == "" {
		return fmt.Errorf("event kind required")
	}
	ev.Text = clip(ev.Text)
	ev.Tool = strings.TrimSpace(ev.Tool)
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[id]
	if !ok || run.Owner != actor {
		return ErrDenied
	}
	if len(run.Events) >= maxEvents {
		if ev.Kind == "done" || ev.Kind == "err" {
			run.Events[maxEvents-1] = ev
			run.EventCount = len(run.Events)
			s.runs[id] = run
		}
		return nil
	}
	run.Events = append(run.Events, ev)
	run.EventCount = len(run.Events)
	s.runs[id] = run
	return nil
}

func (s *Store) FinishRun(id, actor, status, errText string) error {
	if id == "" || actor == "" {
		return ErrDenied
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[id]
	if !ok || run.Owner != actor {
		return ErrDenied
	}
	if status == "" {
		status = "done"
	}
	run.Status = status
	run.Err = clip(errText)
	run.EndedAt = nowRFC3339()
	s.runs[id] = run
	s.evictRunsLocked()
	return nil
}

// evictRunsLocked keeps the newest maxRuns finished runs; running runs are
// never evicted. Called with s.mu held.
func (s *Store) evictRunsLocked() {
	finished := 0
	for _, r := range s.runs {
		if r.EndedAt != "" {
			finished++
		}
	}
	if finished <= maxRuns {
		return
	}
	// Collect finished run ids sorted oldest-first by end time (id tiebreak).
	ids := make([]string, 0, finished)
	for id, r := range s.runs {
		if r.EndedAt != "" {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		ri, rj := s.runs[ids[i]], s.runs[ids[j]]
		if ri.EndedAt == rj.EndedAt {
			return ids[i] < ids[j]
		}
		return ri.EndedAt < rj.EndedAt
	})
	drop := finished - maxRuns
	for _, id := range ids[:drop] {
		delete(s.runs, id)
	}
}

func (s *Store) GetRun(id, actor string) (Run, error) {
	if id == "" || actor == "" {
		return Run{}, ErrDenied
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	if run.Owner != actor {
		return Run{}, ErrDenied
	}
	run.Events = append([]Event(nil), run.Events...)
	return run, nil
}

func (s *Store) ListEvents(id, actor string) ([]Event, error) {
	run, err := s.GetRun(id, actor)
	if err != nil {
		return nil, err
	}
	return run.Events, nil
}

// HasRunningRun reports whether the bot has a run in "running" status. The
// scheduler uses it to skip firing a routine whose previous run is still
// going, so a hung run never piles up concurrent fires.
func (s *Store) HasRunningRun(botID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, run := range s.runs {
		if run.BotID == botID && run.Status == "running" {
			return true
		}
	}
	return false
}

// AnyRunningRun reports whether any run is in "running" status. Diff
// review uses it to fail closed when a concurrent run could be writing
// the same workspace.
func (s *Store) AnyRunningRun() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, run := range s.runs {
		if run.Status == "running" {
			return true
		}
	}
	return false
}

func (s *Store) ListRuns(actor string) []Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Run, 0, len(s.runs))
	for _, run := range s.runs {
		if run.Owner == actor {
			run.Events = nil
			out = append(out, run)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func newID(prefix string) string {
	var b [idRandSize]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("store: crypto/rand: " + err.Error())
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

func clip(s string) string {
	if len(s) <= textCap {
		return s
	}
	s = s[:textCap]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
