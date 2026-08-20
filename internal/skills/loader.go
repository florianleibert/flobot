// Package skills discovers local SKILL.md files and prepends the selected
// skill's body to a user prompt before it is handed to the executor.
//
// A skill is a directory containing a SKILL.md file, optionally with a
// YAML front-matter block delimited by "---" lines (name, description,
// ...). The body is the markdown after the front-matter, verbatim.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Separator joins the skill body and the user prompt.
const Separator = "\n\n---\n\n"

// Skill is a discovered SKILL.md file.
type Skill struct {
	// Name is the skill name: the "name" front-matter field if present,
	// otherwise the directory containing the SKILL.md file.
	Name string
	// Description is the "description" front-matter field, if present.
	Description string
	// Body is the markdown body of the SKILL.md file (front-matter
	// stripped), verbatim.
	Body string
	// Path is the absolute path to the SKILL.md file.
	Path string
}

// Discover scans dir for SKILL.md files, one level deep (dir/*/SKILL.md).
// Results are sorted by skill name. A missing or unreadable dir yields an
// error; a dir with no skills yields an empty slice and nil error.
func Discover(dir string) ([]Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("skills: discover %s: %w", dir, err)
	}

	var out []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("skills: read %s: %w", path, err)
		}

		name, description, body := parseSkill(string(data))
		if name == "" {
			name = e.Name()
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = path
		}
		out = append(out, Skill{
			Name:        name,
			Description: description,
			Body:        body,
			Path:        abs,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Find returns the skill named name from skills, or nil if absent.
func Find(skills []Skill, name string) *Skill {
	for i := range skills {
		if skills[i].Name == name {
			return &skills[i]
		}
	}
	return nil
}

// Apply prepends the selected skill's body to userPrompt:
//
//	skillBody + "\n\n---\n\n" + userPrompt
//
// If skill is nil, userPrompt is returned unmodified.
func Apply(skill *Skill, userPrompt string) string {
	if skill == nil {
		return userPrompt
	}
	return skill.Body + Separator + userPrompt
}

// parseSkill splits a SKILL.md file into its front-matter fields and body.
// Front-matter is a leading block delimited by "---" lines; only the
// "name" and "description" fields are extracted. The body is everything
// after the closing delimiter, trimmed of surrounding blank lines.
func parseSkill(content string) (name, description, body string) {
	lines := strings.Split(content, "\n")

	// Find the opening delimiter: the first non-blank line must be "---".
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || strings.TrimSpace(lines[i]) != "---" {
		return "", "", strings.TrimSpace(content)
	}

	// Find the closing delimiter.
	end := -1
	for j := i + 1; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "---" {
			end = j
			break
		}
	}
	if end == -1 {
		// Unterminated front-matter: treat the whole file as body.
		return "", "", strings.TrimSpace(content)
	}

	for _, line := range lines[i+1 : end] {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "name":
			name = value
		case "description":
			description = value
		}
	}

	body = strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	return name, description, body
}
