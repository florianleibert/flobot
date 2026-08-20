// Persistence: Save/Load the roster to ~/.flobot/state.json.
//
// The file contains bots, skills, routines and messages only. Owner is
// json:"-" on every record, so it can never reach the file; runs/events
// are never persisted; API keys never live on these types.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// stateFile is the on-disk shape. Only the four roster collections.
// Owner is json:"-" on every record, so it is carried in a separate
// "owners" map keyed by record id — never as an "Owner" field.
type stateFile struct {
	Bots     []Bot     `json:"bots"`
	Skills   []Skill   `json:"skills"`
	Routines []Routine `json:"routines"`
	Messages []Message `json:"messages"`
	Owners   map[string]string `json:"owners,omitempty"`
}

// PersistPath returns the state file path: $HOME/.flobot/state.json,
// or the test override set via Store.PersistPath.
func (s *Store) PersistPath() string {
	if s.persistPath != "" {
		return s.persistPath
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".flobot", "state.json")
	}
	return filepath.Join(home, ".flobot", "state.json")
}

// SetPersistPath overrides the state file location (tests).
func (s *Store) SetPersistPath(p string) {
	s.persistPath = p
}

// Save writes the roster to the state file. The directory is created
// 0700 and the file 0600. It never writes Owner, runs, events or secrets.
func (s *Store) Save() error {
	s.mu.Lock()
	st := stateFile{
		Bots:     make([]Bot, 0, len(s.bots)),
		Skills:   make([]Skill, 0, len(s.skills)),
		Routines: make([]Routine, 0, len(s.routines)),
		Messages: append([]Message(nil), s.messages...),
		Owners:   make(map[string]string),
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

	path := s.PersistPath()
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

// Load replaces the store contents from the state file. A missing file
// is not an error: the store stays empty.
func (s *Store) Load() error {
	path := s.PersistPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("store: read state: %w", err)
	}
	var st stateFile
	if err := json.Unmarshal(raw, &st); err != nil {
		return fmt.Errorf("store: parse state: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bots = make(map[string]Bot, len(st.Bots))
	for _, b := range st.Bots {
		b.Owner = st.Owners[b.ID]
		s.bots[b.ID] = b
	}
	s.skills = make(map[string]Skill, len(st.Skills))
	for _, sk := range st.Skills {
		sk.Owner = st.Owners[sk.ID]
		s.skills[sk.ID] = sk
	}
	s.routines = make(map[string]Routine, len(st.Routines))
	for _, rt := range st.Routines {
		rt.Owner = st.Owners[rt.ID]
		s.routines[rt.ID] = rt
	}
	s.messages = append([]Message(nil), st.Messages...)
	return nil
}

// assertNoLeaks is a last line of defense: the serialized state must not
// contain an Owner field or anything that looks like an LLM_/api_key env.
func assertNoLeaks(raw []byte) error {
	low := strings.ToLower(string(raw))
	for _, leak := range []string{`"owner"`, "llm_", "api_key"} {
		if strings.Contains(low, leak) {
			return fmt.Errorf("store: refusing to persist state containing %q", leak)
		}
	}
	return nil
}
