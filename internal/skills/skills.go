// Package skills turns a skill from a blob of prose into a contract, and keeps
// the prompt cheap by injecting only the index.
//
// Two ideas from the research drive this:
//
//   - A skill is a contract: when to use it, the procedure, the pitfalls, and
//     how to verify it worked. Free-text "how-to" bodies are what make an agent
//     follow a skill it should not have used.
//   - Progressive disclosure: the prompt carries one line per skill (name plus
//     when-to-use) and the body is fetched only when the model asks for it.
//     Injecting every body is how five skills eat the whole context window.
package skills

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// maxIndexLine bounds one index entry so a long when-to-use cannot turn
	// the index into the thing it was meant to replace.
	maxIndexLine = 160

	// maxField bounds one contract field.
	maxField = 4 << 10
)

// Status values for a skill. A draft is inert: it never reaches a prompt and
// never fires a routine until a person approves it.
const (
	StatusDraft = "draft"
	StatusLive  = "live"
)

// Contract is the structured part of a skill. Body is kept as the fallback for
// skills authored before the contract existed, so no existing row is lost.
type Contract struct {
	Name         string
	WhenToUse    string
	Procedure    string
	Pitfalls     string
	Verification string
	Body         string
}

// Complete reports whether the contract has the fields a live skill needs. A
// skill with only a Body is legacy-complete: it can still run, but it cannot
// appear in the index with a when-to-use line.
func (c Contract) Complete() bool {
	return strings.TrimSpace(c.Name) != "" && strings.TrimSpace(c.WhenToUse) != ""
}

// IndexLine renders the one-line index entry: name plus when-to-use,
// falling back to the first line of the body for a legacy skill.
func (c Contract) IndexLine() string {
	name := strings.TrimSpace(c.Name)
	when := strings.TrimSpace(c.WhenToUse)
	if when == "" {
		when = firstLine(c.Body)
	}
	when = clipText(strings.Join(strings.Fields(when), " "), maxIndexLine)
	if when == "" {
		return "- " + name
	}
	return "- " + name + " — " + when
}

// Render formats the full contract for injection once the model asks for it.
func (c Contract) Render() string {
	var b strings.Builder
	if name := strings.TrimSpace(c.Name); name != "" {
		b.WriteString("# Skill: " + name + "\n")
	}
	writeField(&b, "When to use", c.WhenToUse)
	writeField(&b, "Procedure", c.Procedure)
	writeField(&b, "Pitfalls", c.Pitfalls)
	writeField(&b, "Verification", c.Verification)
	// A legacy skill has no contract fields; its body is the whole skill.
	if strings.TrimSpace(c.WhenToUse) == "" && strings.TrimSpace(c.Procedure) == "" {
		if body := strings.TrimSpace(c.Body); body != "" {
			b.WriteString("\n" + body + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func writeField(b *strings.Builder, label, value string) {
	v := strings.TrimSpace(value)
	if v == "" {
		return
	}
	b.WriteString("\n" + label + ":\n" + clipText(v, maxField) + "\n")
}

// Index renders the prompt block for a set of skills: one line each, sorted by
// name so the prefix is stable across runs (a stable prefix is what lets a
// local model reuse its prompt cache).
func Index(list []Contract) string {
	if len(list) == 0 {
		return ""
	}
	sorted := append([]Contract(nil), list...)
	sort.Slice(sorted, func(i, j int) bool {
		return strings.ToLower(sorted[i].Name) < strings.ToLower(sorted[j].Name)
	})
	lines := make([]string, 0, len(sorted))
	for _, c := range sorted {
		if strings.TrimSpace(c.Name) == "" {
			continue
		}
		lines = append(lines, c.IndexLine())
	}
	if len(lines) == 0 {
		return ""
	}
	return "Skills you have (ask for one by name to load its procedure):\n" +
		strings.Join(lines, "\n") + "\n\n"
}

// Validate checks a contract before it can go live.
func Validate(c Contract) error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("skill name required")
	}
	if !c.Complete() && strings.TrimSpace(c.Body) == "" {
		return fmt.Errorf("skill needs a when-to-use line or a body")
	}
	return nil
}

// firstLine returns the first non-blank line of s.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// clipText truncates to n bytes without splitting a rune.
func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
