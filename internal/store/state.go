// State persistence: the roster lives in ~/.flobot/state.json.
//
// The file is written 0600 (directory 0700) and never contains an Owner
// field or any secret. On first load the roster is initialized with the
// named teammates glm, qwen and muse, each pinned to its model. A
// corrupted state file is handled gracefully: the error is returned for
// the caller to log, the store falls back to the pinned defaults, and the
// process never crashes.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/florianleibert/flobot/internal/routes"
)

// stateFile is the on-disk shape. Only the four roster collections.
// Owner is json:"-" on every record, so it is carried in a separate
// "owners" map keyed by record id — never as an "Owner" field.
type stateFile struct {
	Bots     []Bot             `json:"bots"`
	Skills   []Skill           `json:"skills"`
	Routines []Routine         `json:"routines"`
	Messages []Message         `json:"messages"`
	Owners   map[string]string `json:"owners,omitempty"`
	// LocalPin is the flocode pin for the "local" route ("" when unpinned).
	// It is a model pin name, not a secret.
	LocalPin string `json:"local_pin,omitempty"`
}

// State is the persisted roster. It is the on-disk authority for bots,
// skills, routines and messages; runs and events are never persisted.
type State struct {
	mu       sync.Mutex
	bots     map[string]Bot
	skills   map[string]Skill
	routines map[string]Routine
	messages []Message

	// path is the state file location; empty means the default
	// ~/.flobot/state.json.
	path string
}

// NewState returns an empty state rooted at the default state file.
func NewState() *State {
	return &State{
		bots:     make(map[string]Bot),
		skills:   make(map[string]Skill),
		routines: make(map[string]Routine),
	}
}

// Path returns the state file path: $HOME/.flobot/state.json, or the
// override set via SetPath. Under go test (FLOBOT_TEST_STATE set), an
// unset path resolves to a temp dir so tests never touch the real home.
func (s *State) Path() string {
	if s.path != "" {
		return s.path
	}
	if p := os.Getenv("FLOBOT_TEST_STATE"); p != "" {
		return filepath.Join(p, "state.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".flobot", "state.json")
	}
	return filepath.Join(home, ".flobot", "state.json")
}

// SetPath overrides the state file location (tests).
func (s *State) SetPath(p string) {
	s.path = p
}

// ListBots returns the bots owned by actor, sorted by creation time then ID.
func (s *State) ListBots(actor string) []Bot {
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

// defaultBots are the named teammates initialized when the roster is
// absent: glm, qwen and muse, each pinned to its model via its route.
func defaultBots() []Bot {
	out := make([]Bot, 0, len(routes.PinMap))
	for _, id := range []string{"glm", "qwen", "muse"} {
		r, ok := routes.Lookup(id)
		if !ok {
			continue
		}
		out = append(out, Bot{
			ID:        "bot-" + id,
			Name:      id,
			Role:      r.Role,
			Route:     id,
			Owner:     ActorLocal,
			CreatedAt: nowRFC3339(),
		})
	}
	return out
}

// Load reads the roster from the state file. A missing file is not an
// error: the roster is initialized with the pinned defaults. A corrupted
// file is handled gracefully: the error is returned (for logging) and the
// store falls back to the pinned defaults instead of crashing.
func (s *State) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.Path())
	switch {
	case err == nil:
		var st stateFile
		if jerr := json.Unmarshal(raw, &st); jerr != nil {
			// Corrupted JSON: keep the pinned defaults, surface the
			// error to the caller, never crash.
			s.applyDefaults()
			return fmt.Errorf("store: state file %s is corrupted, using defaults: %w", s.Path(), jerr)
		}
		s.apply(st)
		return nil
	case errors.Is(err, os.ErrNotExist):
		s.applyDefaults()
		return nil
	default:
		s.applyDefaults()
		return fmt.Errorf("store: read state: %w", err)
	}
}

// apply installs a parsed state file, backfilling owners from the owners
// map. Records without an owner are attributed to the local actor.
func (s *State) apply(st stateFile) {
	s.bots = make(map[string]Bot, len(st.Bots))
	for _, b := range st.Bots {
		b.Owner = ownerOf(st.Owners, b.ID)
		s.bots[b.ID] = b
	}
	s.skills = make(map[string]Skill, len(st.Skills))
	for _, sk := range st.Skills {
		sk.Owner = ownerOf(st.Owners, sk.ID)
		s.skills[sk.ID] = sk
	}
	s.routines = make(map[string]Routine, len(st.Routines))
	for _, rt := range st.Routines {
		rt.Owner = ownerOf(st.Owners, rt.ID)
		s.routines[rt.ID] = rt
	}
	s.messages = append([]Message(nil), st.Messages...)
	routes.SetLocalPin(st.LocalPin)
}

// applyDefaults seeds the roster with the pinned glm/qwen/muse teammates.
func (s *State) applyDefaults() {
	routes.SetLocalPin("")
	s.bots = make(map[string]Bot)
	for _, b := range defaultBots() {
		s.bots[b.ID] = b
	}
	s.skills = make(map[string]Skill)
	s.routines = make(map[string]Routine)
	s.messages = nil
}

func ownerOf(owners map[string]string, id string) string {
	if o := owners[id]; o != "" {
		return o
	}
	return ActorLocal
}

// Save writes the roster to the state file. The directory is created 0700
// and the file 0600. It never writes Owner, runs, events or secrets.
func (s *State) Save() error {
	s.mu.Lock()
	st := stateFile{
		Bots:     make([]Bot, 0, len(s.bots)),
		Skills:   make([]Skill, 0, len(s.skills)),
		Routines: make([]Routine, 0, len(s.routines)),
		Messages: append([]Message(nil), s.messages...),
		Owners:   make(map[string]string),
		LocalPin: routes.LocalPin(),
	}
	for _, b := range s.bots {
		st.Bots = append(st.Bots, b)
		st.Owners[b.ID] = b.Owner
	}
	for _, sk := range s.skills {
		st.Skills = append(st.Skills, sk)
		st.Owners[sk.ID] = sk.Owner
	}
	for _, rt := range s.routines {
		st.Routines = append(st.Routines, rt)
		st.Owners[rt.ID] = rt.Owner
	}
	s.mu.Unlock()

	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("store: marshal state: %w", err)
	}
	if err := assertNoLeaks(raw); err != nil {
		return err
	}

	path := s.Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("store: mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("store: write state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("store: rename state: %w", err)
	}
	return nil
}

// assertNoLeaks is a last line of defense: the serialized state must not
// contain an Owner field or anything that looks like a secret.
func assertNoLeaks(raw []byte) error {
	low := strings.ToLower(string(raw))
	for _, leak := range []string{`"owner"`, "llm_", "api_key", "secret"} {
		if strings.Contains(low, leak) {
			return fmt.Errorf("store: refusing to persist state containing %q", leak)
		}
	}
	return nil
}
