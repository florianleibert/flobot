package server

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/florianleibert/flobot/internal/store"
)

// projectStore returns the persisted projects/tags store.
func (s *Server) projectStore() *store.ProjectStore {
	s.psOnce.Do(func() {
		dir := s.home
		if dir == "" {
			dir, _ = os.UserHomeDir()
		}
		if dir == "" {
			dir = "."
		}
		s.projects = store.NewProjectStore(filepath.Join(s.home, ".flobot", "projects.json"))
		if s.home == "" {
			// DefaultProjectsPath honours FLOBOT_TEST_STATE, so tests never
			// write into the user's real ~/.flobot.
			s.projects = store.NewProjectStore(store.DefaultProjectsPath())
		}
		if err := s.projects.Load(); err != nil {
			log.Printf("flobot: load projects: %v", err)
		}
	})
	return s.projects
}

// listProjects serves GET /api/v1/projects.
//
// ?tag=NAME returns only the projects carrying that tag — the tag view of
// the Code panel. The tag vocabulary travels with the list so the client
// never has to make a second request to draw the tag strip.
func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	ps := s.projectStore()
	writeJSON(w, map[string]any{
		"schema":   "flobot.projects.v1",
		"projects": ps.List(tag),
		"tags":     ps.Tags(),
	})
}

// createProject serves POST /api/v1/projects.
//
// A project is a name, a workspace-relative root folder, the set of
// files/folders in scope, and the tags on it. Tags that are new are added to
// the vocabulary, the way a new tag appears when you type it in Photos.
func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string   `json:"name"`
		Root  string   `json:"root"`
		Paths []string `json:"paths"`
		Tags  []string `json:"tags"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	p, err := s.projectStore().Create(store.Project{
		Name:  body.Name,
		Root:  body.Root,
		Paths: body.Paths,
		Tags:  body.Tags,
	})
	if err != nil {
		s.httpError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, p)
}

// patchProject serves PATCH /api/v1/projects/{id}.
//
// Only the fields present in the body change: naming a project, moving its
// root, replacing its scope, or replacing its tags. Tags are how a project
// joins a group, so tagging is a plain PATCH — one call, no special verbs.
func (s *Server) patchProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Name  *string   `json:"name"`
		Root  *string   `json:"root"`
		Paths *[]string `json:"paths"`
		Tags  *[]string `json:"tags"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	p, found, err := s.projectStore().Update(id, func(cur *store.Project) bool {
		changed := false
		if body.Name != nil {
			name := strings.TrimSpace(*body.Name)
			if name != "" && name != cur.Name {
				cur.Name = name
				changed = true
			}
		}
		if body.Root != nil && *body.Root != cur.Root {
			cur.Root = *body.Root
			changed = true
		}
		if body.Paths != nil {
			cur.Paths = *body.Paths
			changed = true
		}
		if body.Tags != nil {
			cur.Tags = *body.Tags
			changed = true
		}
		return changed
	})
	if err != nil {
		s.httpError(w, err)
		return
	}
	if !found {
		s.httpError(w, store.ErrNotFound)
		return
	}
	writeJSON(w, p)
}

// deleteProject serves DELETE /api/v1/projects/{id}.
func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	if err := s.projectStore().Delete(r.PathValue("id")); err != nil {
		s.httpError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listTags serves GET /api/v1/tags.
//
// Each tag carries the number of projects wearing it, so the Code panel can
// show "api · 3" and hide labels nobody uses.
func (s *Server) listTags(w http.ResponseWriter, _ *http.Request) {
	ps := s.projectStore()
	projects := ps.List("")
	out := make([]map[string]any, 0, len(ps.Tags()))
	for _, t := range ps.Tags() {
		n := 0
		for _, p := range projects {
			for _, pt := range p.Tags {
				if strings.EqualFold(pt, t.Name) {
					n++
					break
				}
			}
		}
		out = append(out, map[string]any{
			"name":      t.Name,
			"createdAt": t.CreatedAt,
			"projects":  n,
		})
	}
	writeJSON(w, map[string]any{"schema": "flobot.tags.v1", "tags": out})
}

// createTag serves POST /api/v1/tags. A tag can also be created implicitly
// by tagging a project; this endpoint is for naming a label up front.
func (s *Server) createTag(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	t, err := s.projectStore().EnsureTag(body.Name)
	if err != nil {
		s.httpError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, t)
}

// deleteTag serves DELETE /api/v1/tags/{name}. The label leaves the
// vocabulary and every project that wore it.
func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request) {
	if err := s.projectStore().DeleteTag(r.PathValue("name")); err != nil {
		s.httpError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// scopeFor resolves a project or a tag into the workspace-relative paths an
// agent should work inside. A project wins when both are given.
func (s *Server) scopeFor(projectID, tag string) (root string, paths []string, label string, err error) {
	projectID = strings.TrimSpace(projectID)
	tag = strings.TrimSpace(tag)
	if projectID == "" && tag == "" {
		return "", nil, "", nil
	}
	return s.projectStore().Scope(projectID, tag)
}

// scopeBlock renders a resolved scope as a prompt constraint.
//
// The scope travels with the mission, so "work on the api tag" reaches the
// agent as an explicit list of files and folders instead of a convention
// the agent has to guess.
func scopeBlock(label, root string, paths []string) string {
	var b strings.Builder
	b.WriteString("SCOPE — work only inside this scope:\n")
	if label != "" {
		b.WriteString("target: " + label + "\n")
	}
	if root != "" {
		b.WriteString("root: " + root + "\n")
	}
	if len(paths) == 0 {
		b.WriteString("files in scope: the whole root folder\n")
	} else {
		b.WriteString("files and folders in scope:\n")
		for _, p := range paths {
			b.WriteString("- " + p + "\n")
		}
	}
	b.WriteString("Do not edit anything outside this scope.\n\n")
	return b.String()
}
