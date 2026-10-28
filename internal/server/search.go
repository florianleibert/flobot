package server

import (
	"net/http"
	"strings"
)

// searchWorkspace serves GET /api/v1/workspace/search.
//
// q is the literal text to find. paths limits the search to those
// workspace-relative files or folders; tag limits it to the scope of every
// project wearing that tag. The scope comes from the same project store the
// Code view uses, so "search inside #frontend" is one click.
func (s *Server) searchWorkspace(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	var scope []string
	if tag := strings.TrimSpace(r.URL.Query().Get("tag")); tag != "" {
		_, tagPaths, _, err := s.scopeFor("", tag)
		if err != nil {
			s.httpError(w, err)
			return
		}
		scope = append(scope, tagPaths...)
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("paths")); raw != "" {
		for _, p := range strings.Split(raw, ",") {
			if p = strings.TrimSpace(p); p != "" {
				scope = append(scope, p)
			}
		}
	}

	ws, err := s.workspaceWS()
	if err != nil {
		wsError(w, err)
		return
	}
	matches, truncated, err := ws.SearchText(q, scope)
	if err != nil {
		s.httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"query":     q,
		"matches":   matches,
		"truncated": truncated,
		"scope":     scope,
	})
}
