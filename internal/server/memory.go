// memory.go is the HTTP surface for bot memory: the fact list with its usage,
// add/patch/remove, the cap, and the dated work log.
//
// Usage is reported on every response so the UI can show a budget instead of
// discovering the limit through a failure.
package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/florianleibert/flobot/internal/memory"
)

// botMemory returns one bot's facts, usage and recent log.
func (s *Server) botMemory(w http.ResponseWriter, r *http.Request) {
	bot, err := s.store.GetBot(r.PathValue("id"), Actor)
	if err != nil {
		s.httpError(w, err)
		return
	}
	f := bot.Facts()
	writeJSON(w, map[string]any{
		"schema":   "flobot.bot.memory.v1",
		"bot_id":   bot.ID,
		"facts":    f.Lines(),
		"used":     f.Used(),
		"cap":      f.Cap(),
		"percent":  f.Percent(),
		"log":      bot.WorkLog().Entries(),
		"log_size": bot.WorkLog().Len(),
	})
}

// addBotMemory appends one fact. A full memory answers 409 with the usage so the
// client can offer to remove something; it never silently drops the fact.
func (s *Server) addBotMemory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	bot, err := s.store.MemoryAdd(r.PathValue("id"), Actor, body.Text)
	if err != nil {
		s.memoryError(w, err)
		return
	}
	s.persist()
	writeJSON(w, bot.View())
}

// patchBotMemory replaces fact i.
func (s *Server) patchBotMemory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Index int    `json:"index"`
		Text  string `json:"text"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	bot, err := s.store.MemoryPatch(r.PathValue("id"), Actor, body.Index, body.Text)
	if err != nil {
		s.memoryError(w, err)
		return
	}
	s.persist()
	writeJSON(w, bot.View())
}

// removeBotMemory drops fact i.
func (s *Server) removeBotMemory(w http.ResponseWriter, r *http.Request) {
	i, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		s.httpError(w, errors.New("fact index required"))
		return
	}
	bot, err := s.store.MemoryRemove(r.PathValue("id"), Actor, i)
	if err != nil {
		s.memoryError(w, err)
		return
	}
	s.persist()
	writeJSON(w, bot.View())
}

// setBotMemoryCap changes a bot's memory budget.
func (s *Server) setBotMemoryCap(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Cap int `json:"cap"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	bot, err := s.store.SetMemoryCap(r.PathValue("id"), Actor, body.Cap)
	if err != nil {
		s.memoryError(w, err)
		return
	}
	s.persist()
	writeJSON(w, bot.View())
}

// logBotWork appends one dated line to a bot's work log.
func (s *Server) logBotWork(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Summary string `json:"summary"`
		RunID   string `json:"run_id"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	bot, err := s.store.LogWork(r.PathValue("id"), Actor, body.RunID, body.Summary)
	if err != nil {
		s.memoryError(w, err)
		return
	}
	s.persist()
	writeJSON(w, bot.View())
}

// memoryError maps a memory failure to a status: full is a conflict the caller
// can resolve, a bad index is not found, everything else is a bad request.
func (s *Server) memoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, memory.ErrFull):
		var full *memory.FullError
		if errors.As(err, &full) {
			writeJSONStatus(w, http.StatusConflict, map[string]any{
				"error":   err.Error(),
				"used":    full.Used,
				"cap":     full.Cap,
				"percent": full.Used * 100 / full.Cap,
			})
			return
		}
	case errors.Is(err, memory.ErrNotFound):
		s.httpError(w, err)
		return
	}
	s.httpError(w, err)
}
