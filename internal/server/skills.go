// skills.go is the HTTP surface for the skill contract: create a contract, read
// one skill's full procedure, approve a draft, and draft a skill from a run.
package server

import (
	"net/http"
	"strings"

	"github.com/florianleibert/flobot/internal/skills"
	"github.com/florianleibert/flobot/internal/store"
)

// skillBody is the request shape shared by create and patch.
type skillBody struct {
	Name         string `json:"name"`
	Body         string `json:"body"`
	WhenToUse    string `json:"when_to_use"`
	Procedure    string `json:"procedure"`
	Pitfalls     string `json:"pitfalls"`
	Verification string `json:"verification"`
}

func (b skillBody) contract() skills.Contract {
	return skills.Contract{
		Name:         b.Name,
		Body:         b.Body,
		WhenToUse:    b.WhenToUse,
		Procedure:    b.Procedure,
		Pitfalls:     b.Pitfalls,
		Verification: b.Verification,
	}
}

// createSkillContract creates a live skill from a structured contract.
func (s *Server) createSkillContract(w http.ResponseWriter, r *http.Request) {
	var body skillBody
	if !decodeJSON(w, r, runBodyLimit, &body) {
		return
	}
	sk, err := s.store.CreateSkillContract(Actor, body.contract())
	if err != nil {
		s.httpError(w, err)
		return
	}
	s.persist()
	writeJSON(w, sk)
}

// getSkill returns one skill plus its rendered contract, which is what the model
// receives when it asks for a skill by name.
func (s *Server) getSkill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sk, err := s.store.GetSkill(id, Actor)
	if err != nil {
		s.httpError(w, err)
		return
	}
	// A draft has no prompt body; the record is still readable.
	rendered, _ := s.store.SkillPromptBody(id, Actor)
	writeJSON(w, map[string]any{
		"schema":   "flobot.skill.v1",
		"skill":    sk,
		"live":     sk.Live(),
		"rendered": rendered,
	})
}

// patchSkill updates a skill's contract fields and bumps its version.
func (s *Server) patchSkill(w http.ResponseWriter, r *http.Request) {
	var body skillBody
	if !decodeJSON(w, r, runBodyLimit, &body) {
		return
	}
	sk, err := s.store.UpdateSkillContract(r.PathValue("id"), Actor, body.contract())
	if err != nil {
		s.httpError(w, err)
		return
	}
	s.persist()
	writeJSON(w, sk)
}

// approveSkill makes a draft live.
func (s *Server) approveSkill(w http.ResponseWriter, r *http.Request) {
	sk, err := s.store.ApproveSkill(r.PathValue("id"), Actor)
	if err != nil {
		s.httpError(w, err)
		return
	}
	s.persist()
	writeJSON(w, sk)
}

// draftSkillFromRun turns a finished run into an inert skill draft. The caller
// supplies the contract fields; anything omitted is derived from the run itself,
// so "save this run as a skill" works with no form filling.
func (s *Server) draftSkillFromRun(w http.ResponseWriter, r *http.Request) {
	var body skillBody
	if !decodeJSON(w, r, runBodyLimit, &body) {
		return
	}
	run, err := s.store.GetRun(r.PathValue("id"), Actor)
	if err != nil {
		s.httpError(w, err)
		return
	}
	c := body.contract()
	if strings.TrimSpace(c.Name) == "" {
		c.Name = "Learned from " + run.ID
	}
	if strings.TrimSpace(c.WhenToUse) == "" {
		c.WhenToUse = "When a task resembles: " + firstLineOf(run.Prompt)
	}
	if strings.TrimSpace(c.Procedure) == "" {
		c.Procedure = procedureFromRun(run)
	}
	if strings.TrimSpace(c.Verification) == "" {
		c.Verification = "The run finished with status " + run.Status + "."
	}
	draft, err := s.store.DraftSkillFromRun(Actor, run.ID, c)
	if err != nil {
		s.httpError(w, err)
		return
	}
	s.persist()
	writeJSONStatus(w, http.StatusCreated, map[string]any{
		"schema": "flobot.skill.draft.v1",
		"skill":  draft,
		"live":   false,
	})
}

// procedureFromRun summarises what the run actually did, as procedure steps.
// Only the event kinds that describe an action become steps; text is trimmed to
// its first line so the procedure stays a procedure.
func procedureFromRun(run store.Run) string {
	var lines []string
	for _, ev := range run.Events {
		switch ev.Kind {
		case "tool":
			if ev.Tool != "" {
				lines = append(lines, "- used "+ev.Tool)
			}
		case "err":
			if t := firstLineOf(ev.Text); t != "" {
				lines = append(lines, "- hit an error: "+t)
			}
		case "done":
			lines = append(lines, "- finished")
		case "stopped":
			lines = append(lines, "- was stopped")
		case "timeout":
			lines = append(lines, "- timed out")
		}
	}
	if len(lines) == 0 {
		return "- (the run produced no tool events to learn from)"
	}
	return strings.Join(lines, "\n")
}

// firstLineOf returns the first non-blank line of s.
func firstLineOf(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}
