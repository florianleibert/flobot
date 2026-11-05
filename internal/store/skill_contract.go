// skill_contract.go is the skill-contract surface: create a contract, approve a
// draft, and draft a skill from a run.
//
// A draft is inert by construction. It is created with StatusDraft, and every
// path that feeds a prompt or fires a routine filters on Live(), so a skill
// learned from a recording cannot become policy without a person approving it.
package store

import (
	"fmt"
	"strings"

	"github.com/florianleibert/flobot/internal/skills"
)

// CreateSkillContract creates a live skill from a structured contract. An empty
// WhenToUse with a Body is accepted as a legacy skill.
func (s *Store) CreateSkillContract(owner string, c skills.Contract) (Skill, error) {
	if owner == "" {
		return Skill{}, fmt.Errorf("owner required")
	}
	if err := skills.Validate(c); err != nil {
		return Skill{}, err
	}
	sk := Skill{
		ID:           newID("skill"),
		Name:         strings.TrimSpace(c.Name),
		Body:         clip(strings.TrimSpace(c.Body)),
		WhenToUse:    clip(strings.TrimSpace(c.WhenToUse)),
		Procedure:    clip(strings.TrimSpace(c.Procedure)),
		Pitfalls:     clip(strings.TrimSpace(c.Pitfalls)),
		Verification: clip(strings.TrimSpace(c.Verification)),
		Status:       skills.StatusLive,
		Version:      1,
		Owner:        owner,
	}
	s.mu.Lock()
	s.skills[sk.ID] = sk
	s.mu.Unlock()
	s.Save()
	return sk, nil
}

// DraftSkillFromRun records a skill learned from a run. The draft is inert: it
// does not appear in any prompt and cannot back a routine until ApproveSkill.
func (s *Store) DraftSkillFromRun(owner, runID string, c skills.Contract) (Skill, error) {
	if owner == "" {
		return Skill{}, fmt.Errorf("owner required")
	}
	if err := skills.Validate(c); err != nil {
		return Skill{}, err
	}
	sk := Skill{
		ID:           newID("skill"),
		Name:         strings.TrimSpace(c.Name),
		Body:         clip(strings.TrimSpace(c.Body)),
		WhenToUse:    clip(strings.TrimSpace(c.WhenToUse)),
		Procedure:    clip(strings.TrimSpace(c.Procedure)),
		Pitfalls:     clip(strings.TrimSpace(c.Pitfalls)),
		Verification: clip(strings.TrimSpace(c.Verification)),
		Status:       skills.StatusDraft,
		DraftedFrom:  strings.TrimSpace(runID),
		Version:      1,
		Owner:        owner,
	}
	s.mu.Lock()
	s.skills[sk.ID] = sk
	s.mu.Unlock()
	s.Save()
	return sk, nil
}

// ApproveSkill makes a draft live. Approving an already-live skill is a no-op so
// the call is idempotent.
func (s *Store) ApproveSkill(id, actor string) (Skill, error) {
	s.mu.Lock()
	sk, ok := s.skills[id]
	if !ok {
		s.mu.Unlock()
		return Skill{}, ErrNotFound
	}
	if sk.Owner != actor {
		s.mu.Unlock()
		return Skill{}, ErrDenied
	}
	if sk.Live() {
		s.mu.Unlock()
		return sk, nil
	}
	if err := skills.Validate(sk.Contract()); err != nil {
		s.mu.Unlock()
		return Skill{}, err
	}
	sk.Status = skills.StatusLive
	s.skills[id] = sk
	s.mu.Unlock()
	s.Save()
	return sk, nil
}

// UpdateSkillContract replaces a skill's contract fields and bumps its version.
// The status is left alone: editing a draft keeps it a draft.
//
// Fields are merged before validation, so a patch that sends only a procedure
// is checked against the skill it produces, not against an empty contract.
func (s *Store) UpdateSkillContract(id, actor string, c skills.Contract) (Skill, error) {
	s.mu.Lock()
	sk, ok := s.skills[id]
	if !ok {
		s.mu.Unlock()
		return Skill{}, ErrNotFound
	}
	if sk.Owner != actor {
		s.mu.Unlock()
		return Skill{}, ErrDenied
	}
	if name := strings.TrimSpace(c.Name); name != "" {
		sk.Name = name
	}
	if body := strings.TrimSpace(c.Body); body != "" {
		sk.Body = clip(body)
	}
	if v := strings.TrimSpace(c.WhenToUse); v != "" {
		sk.WhenToUse = clip(v)
	}
	if v := strings.TrimSpace(c.Procedure); v != "" {
		sk.Procedure = clip(v)
	}
	if v := strings.TrimSpace(c.Pitfalls); v != "" {
		sk.Pitfalls = clip(v)
	}
	if v := strings.TrimSpace(c.Verification); v != "" {
		sk.Verification = clip(v)
	}
	if err := skills.Validate(sk.Contract()); err != nil {
		s.mu.Unlock()
		return Skill{}, err
	}
	sk.Version++
	s.skills[id] = sk
	s.mu.Unlock()
	s.Save()
	return sk, nil
}

// SkillIndex renders the progressive-disclosure prompt block for the live
// skills of one owner: one line each, no bodies.
func (s *Store) SkillIndex(actor string) string {
	var list []skills.Contract
	for _, sk := range s.ListSkills(actor) {
		if !sk.Live() {
			continue
		}
		list = append(list, sk.Contract())
	}
	return skills.Index(list)
}

// SkillPromptBody returns the full contract for one skill, which is what the
// model gets when it asks for a skill by name.
func (s *Store) SkillPromptBody(id, actor string) (string, error) {
	sk, err := s.GetSkill(id, actor)
	if err != nil {
		return "", err
	}
	if !sk.Live() {
		return "", fmt.Errorf("skill %s is a draft; approve it before use", sk.ID)
	}
	return sk.Contract().Render(), nil
}
