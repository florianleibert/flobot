// projects.go persists the user's projects and tags.
//
// The Code view is organised by projects, not by raw file trees: a project
// names a folder and the set of files/folders inside it that matter right
// now. Tags are global labels (Apple Photos style) — a project can carry
// several, and a tag can select many projects at once. Agents can be scoped
// to a project or to a tag, which is how "work on this label" becomes a
// real instruction instead of a convention.
//
// Same persistence contract as the other stores: 0600 file, 0700 directory,
// atomic temp-file + rename, no secrets in the document.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// maxProjects caps the project list so the file cannot grow forever.
	maxProjects = 200
	// maxPathsPerProject caps the selected file/folder set of one project.
	maxPathsPerProject = 500
	// maxTagsPerProject caps how many labels one project can carry.
	maxTagsPerProject = 50
	// maxTags caps the global tag vocabulary.
	maxTags = 200
)

// Project is a named group of files and folders.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Root is the workspace-relative folder the project lives in. "" and "."
	// both mean the workspace root.
	Root string `json:"root"`
	// Paths are the workspace-relative files and folders in scope.
	Paths []string `json:"paths"`
	// Tags are the labels on this project. Empty means untagged.
	Tags      []string `json:"tags"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
}

// Tag is a global label. A tag exists as soon as it is used on a project.
type Tag struct {
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

// projectFile is the on-disk shape of ~/.flobot/projects.json.
type projectFile struct {
	Projects []Project `json:"projects"`
	Tags     []Tag     `json:"tags"`
}

// ProjectStore holds the projects and tags.
type ProjectStore struct {
	mu       sync.Mutex
	path     string
	projects []Project
	tags     []Tag
}

// NewProjectStore returns a project store at path.
func NewProjectStore(path string) *ProjectStore {
	return &ProjectStore{path: path}
}

// DefaultProjectsPath returns $HOME/.flobot/projects.json (or a test override).
func DefaultProjectsPath() string {
	if p := os.Getenv("FLOBOT_TEST_STATE"); p != "" {
		return filepath.Join(p, "projects.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".flobot", "projects.json")
	}
	return filepath.Join(home, ".flobot", "projects.json")
}

// Load reads the persisted projects and tags. A missing file is not an error.
func (ps *ProjectStore) Load() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	raw, err := os.ReadFile(ps.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var f projectFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("projectstore: unmarshal: %w", err)
	}
	ps.projects = f.Projects
	ps.tags = f.Tags
	return nil
}

// Save persists the projects and tags atomically.
func (ps *ProjectStore) Save() error {
	ps.mu.Lock()
	if len(ps.projects) > maxProjects {
		ps.projects = ps.projects[len(ps.projects)-maxProjects:]
	}
	f := projectFile{
		Projects: append([]Project(nil), ps.projects...),
		Tags:     append([]Tag(nil), ps.tags...),
	}
	ps.mu.Unlock()
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := assertNoLeaks(raw); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ps.path), 0o700); err != nil {
		return err
	}
	tmp := ps.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, ps.path)
}

// NormalizeTag trims a tag name and drops empty ones.
func NormalizeTag(name string) string {
	return strings.TrimSpace(name)
}

// sameTag compares tag names case-insensitively.
func sameTag(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// List returns the projects, newest first. When tag is set, only projects
// carrying that tag are returned.
func (ps *ProjectStore) List(tag string) []Project {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	out := make([]Project, 0, len(ps.projects))
	for _, p := range ps.projects {
		if tag != "" && !hasTag(p.Tags, tag) {
			continue
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if sameTag(t, want) {
			return true
		}
	}
	return false
}

// Get returns one project by id.
func (ps *ProjectStore) Get(id string) (Project, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for _, p := range ps.projects {
		if p.ID == id {
			return p, true
		}
	}
	return Project{}, false
}

// Create adds a project and registers any new tags it carries.
func (ps *ProjectStore) Create(p Project) (Project, error) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return Project{}, fmt.Errorf("project name required")
	}
	if p.ID == "" {
		id, err := randomID(4)
		if err != nil {
			return Project{}, err
		}
		p.ID = id
	}
	now := time.Now().UTC().Format(time.RFC3339)
	p.CreatedAt = now
	p.UpdatedAt = now
	p.Root = normalizeRel(p.Root)
	p.Paths = normalizePaths(p.Paths)
	p.Tags = normalizeTags(p.Tags)

	ps.mu.Lock()
	for _, existing := range ps.projects {
		if existing.ID == p.ID {
			ps.mu.Unlock()
			return Project{}, fmt.Errorf("project %s already exists", p.ID)
		}
	}
	ps.projects = append(ps.projects, p)
	ps.addTagsLocked(p.Tags)
	ps.mu.Unlock()
	if err := ps.Save(); err != nil {
		return Project{}, err
	}
	return p, nil
}

// Update mutates one project in place. fn reports whether the project was
// changed; when it returns false nothing is written.
func (ps *ProjectStore) Update(id string, fn func(*Project) bool) (Project, bool, error) {
	ps.mu.Lock()
	found := false
	changed := false
	var updated Project
	for i := range ps.projects {
		if ps.projects[i].ID != id {
			continue
		}
		found = true
		updated = ps.projects[i]
		if fn(&updated) {
			updated.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			updated.Root = normalizeRel(updated.Root)
			updated.Paths = normalizePaths(updated.Paths)
			updated.Tags = normalizeTags(updated.Tags)
			ps.projects[i] = updated
			// A new label joins the vocabulary the moment a project wears it.
			ps.addTagsLocked(updated.Tags)
			changed = true
		}
		break
	}
	ps.mu.Unlock()
	if !found {
		return Project{}, false, nil
	}
	if changed {
		if err := ps.Save(); err != nil {
			return Project{}, true, err
		}
	}
	return updated, true, nil
}

// Delete removes a project.
func (ps *ProjectStore) Delete(id string) error {
	ps.mu.Lock()
	idx := -1
	for i, p := range ps.projects {
		if p.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		ps.mu.Unlock()
		return ErrNotFound
	}
	ps.projects = append(ps.projects[:idx], ps.projects[idx+1:]...)
	ps.mu.Unlock()
	return ps.Save()
}

// Tags returns the tag vocabulary, alphabetically.
func (ps *ProjectStore) Tags() []Tag {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	out := append([]Tag(nil), ps.tags...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// EnsureTag registers a tag if it is new and returns it.
func (ps *ProjectStore) EnsureTag(name string) (Tag, error) {
	name = NormalizeTag(name)
	if name == "" {
		return Tag{}, fmt.Errorf("tag name required")
	}
	ps.mu.Lock()
	for _, t := range ps.tags {
		if sameTag(t.Name, name) {
			ps.mu.Unlock()
			return t, nil
		}
	}
	t := Tag{Name: name, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	ps.tags = append(ps.tags, t)
	ps.mu.Unlock()
	if err := ps.Save(); err != nil {
		return Tag{}, err
	}
	return t, nil
}

// DeleteTag removes a tag from the vocabulary and from every project.
func (ps *ProjectStore) DeleteTag(name string) error {
	ps.mu.Lock()
	kept := ps.tags[:0]
	found := false
	for _, t := range ps.tags {
		if sameTag(t.Name, name) {
			found = true
			continue
		}
		kept = append(kept, t)
	}
	ps.tags = kept
	for i := range ps.projects {
		ps.projects[i].Tags = removeTag(ps.projects[i].Tags, name)
	}
	ps.mu.Unlock()
	if !found {
		return ErrNotFound
	}
	return ps.Save()
}

// addTagsLocked registers tags in the vocabulary. The caller holds the lock.
func (ps *ProjectStore) addTagsLocked(names []string) {
	for _, n := range names {
		if n == "" {
			continue
		}
		known := false
		for _, t := range ps.tags {
			if sameTag(t.Name, n) {
				known = true
				break
			}
		}
		if known {
			continue
		}
		if len(ps.tags) >= maxTags {
			return
		}
		ps.tags = append(ps.tags, Tag{Name: n, CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	}
}

// Scope returns the workspace-relative paths a project or a tag covers.
// A project wins over a tag when both are given.
func (ps *ProjectStore) Scope(projectID, tag string) (root string, paths []string, label string, err error) {
	if projectID != "" {
		p, ok := ps.Get(projectID)
		if !ok {
			return "", nil, "", ErrNotFound
		}
		return p.Root, append([]string(nil), p.Paths...), p.Name, nil
	}
	if tag == "" {
		return "", nil, "", nil
	}
	projects := ps.List(tag)
	if len(projects) == 0 {
		return "", nil, "", ErrNotFound
	}
	seen := map[string]bool{}
	for _, p := range projects {
		for _, path := range p.Paths {
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return "", paths, tag, nil
}

// normalizeRel cleans a workspace-relative folder path. It never returns an
// absolute path or a parent-traversing one: the workspace owns the root.
func normalizeRel(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.Trim(p, "/")
	if p == "" || p == "." {
		return ""
	}
	if strings.HasPrefix(p, "/") {
		return ""
	}
	parts := strings.Split(p, "/")
	clean := parts[:0]
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			return ""
		}
		clean = append(clean, part)
	}
	return strings.Join(clean, "/")
}

// normalizePaths cleans, de-duplicates and caps the scope paths.
func normalizePaths(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		p := normalizeRel(raw)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
		if len(out) >= maxPathsPerProject {
			break
		}
	}
	sort.Strings(out)
	return out
}

// normalizeTags cleans, de-duplicates and caps a tag list.
func normalizeTags(tags []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, raw := range tags {
		t := NormalizeTag(raw)
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
		if len(out) >= maxTagsPerProject {
			break
		}
	}
	return out
}

// removeTag drops a tag from a list.
func removeTag(tags []string, name string) []string {
	out := tags[:0]
	for _, t := range tags {
		if sameTag(t, name) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// randomID returns a short random hex id.
func randomID(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
