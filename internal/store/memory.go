// memory.go is the bot-memory surface: add, patch and remove a fact, and
// append to the dated work log.
//
// These live in their own file so the roster file stays about the roster. Every
// mutation is server-authoritative: the caller names what it wants changed and
// the store decides whether it fits, so an agent cannot talk its way past the
// budget.
package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/florianleibert/flobot/internal/memory"
)

// MemoryAdd appends one fact to a bot's memory. It refuses with
// memory.ErrFull (carrying the usage) instead of clipping, so the caller can
// remove something first.
func (s *Store) MemoryAdd(id, actor, text string) (Bot, error) {
	return s.mutateFacts(id, actor, func(f memory.Facts) (memory.Facts, error) {
		return f.Add(text)
	})
}

// MemoryPatch replaces fact i.
func (s *Store) MemoryPatch(id, actor string, i int, text string) (Bot, error) {
	return s.mutateFacts(id, actor, func(f memory.Facts) (memory.Facts, error) {
		return f.Patch(i, text)
	})
}

// MemoryRemove drops fact i.
func (s *Store) MemoryRemove(id, actor string, i int) (Bot, error) {
	return s.mutateFacts(id, actor, func(f memory.Facts) (memory.Facts, error) {
		return f.Remove(i)
	})
}

// mutateFacts applies one fact-set change under the store lock and persists on
// success. A refused change leaves the bot untouched.
func (s *Store) mutateFacts(id, actor string, fn func(memory.Facts) (memory.Facts, error)) (Bot, error) {
	if id == "" || actor == "" {
		return Bot{}, ErrDenied
	}
	s.mu.Lock()
	bot, ok := s.bots[id]
	if !ok {
		s.mu.Unlock()
		return Bot{}, ErrNotFound
	}
	if bot.Owner != actor {
		s.mu.Unlock()
		return Bot{}, ErrDenied
	}
	next, err := fn(bot.Facts())
	if err != nil {
		s.mu.Unlock()
		return Bot{}, err
	}
	bot.Memory = next.String()
	bot.MemoryCap = next.Cap()
	s.bots[id] = bot
	s.mu.Unlock()
	s.Save()
	return bot, nil
}

// SetMemoryCap changes a bot's memory budget. Shrinking below current usage is
// allowed: the facts stay and the next add reports the real overage.
func (s *Store) SetMemoryCap(id, actor string, capacity int) (Bot, error) {
	if capacity <= 0 {
		return Bot{}, fmt.Errorf("memory cap must be positive")
	}
	return s.mutateFacts(id, actor, func(f memory.Facts) (memory.Facts, error) {
		return f.WithCap(capacity), nil
	})
}

// LogWork records one dated line of what the bot did. runID may be empty.
func (s *Store) LogWork(id, actor, runID, summary string) (Bot, error) {
	if id == "" || actor == "" {
		return Bot{}, ErrDenied
	}
	s.mu.Lock()
	bot, ok := s.bots[id]
	if !ok {
		s.mu.Unlock()
		return Bot{}, ErrNotFound
	}
	if bot.Owner != actor {
		s.mu.Unlock()
		return Bot{}, ErrDenied
	}
	if strings.TrimSpace(summary) == "" {
		s.mu.Unlock()
		return Bot{}, fmt.Errorf("log summary required")
	}
	next := bot.WorkLog().Append(nowRFC3339(), runID, summary)
	bot.Log = next.Entries()
	s.bots[id] = bot
	s.mu.Unlock()
	s.Save()
	return bot, nil
}

// LogWorkAt is LogWork with an explicit timestamp (tests and backfill).
func (s *Store) LogWorkAt(id, actor, at, runID, summary string) (Bot, error) {
	if id == "" || actor == "" {
		return Bot{}, ErrDenied
	}
	if at == "" {
		at = time.Now().UTC().Format(time.RFC3339)
	}
	s.mu.Lock()
	bot, ok := s.bots[id]
	if !ok {
		s.mu.Unlock()
		return Bot{}, ErrNotFound
	}
	if bot.Owner != actor {
		s.mu.Unlock()
		return Bot{}, ErrDenied
	}
	if strings.TrimSpace(summary) == "" {
		s.mu.Unlock()
		return Bot{}, fmt.Errorf("log summary required")
	}
	next := bot.WorkLog().Append(at, runID, summary)
	bot.Log = next.Entries()
	s.bots[id] = bot
	s.mu.Unlock()
	s.Save()
	return bot, nil
}

// MemoryPrompt renders the block prepended to a run's prompt: the facts, then
// the most recent log lines. It returns "" when the bot has neither, so a fresh
// bot's prompt is unchanged.
func MemoryPrompt(b Bot) string {
	facts := b.Facts().String()
	log := b.WorkLog().Render(recentLogLines)
	var parts []string
	if facts != "" {
		parts = append(parts, "Facts you keep (curate these; they ride in every run):\n"+facts)
	}
	if log != "" {
		parts = append(parts, "What you did recently:\n"+log)
	}
	if len(parts) == 0 {
		return ""
	}
	return "Teammate memory:\n" + strings.Join(parts, "\n\n") + "\n\n"
}

// recentLogLines is how much of the work log rides in the prompt. The whole log
// is available through the API; only the tail is worth prompt budget.
const recentLogLines = 10
