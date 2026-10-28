package server

import (
	"net/http"
)

// workspaceSymbols handles GET /api/v1/workspace/symbols?q=<name>.
//
// It is the workspace-wide "go to symbol" surface (Codex desktop parity,
// requirement R-EDIT-a). The open-file outline in the editor only covers
// the file on screen; this searches the whole workspace so a symbol can be
// found without knowing which file declares it.
//
// The response is always a list, never an error for "nothing matched": an
// empty query returns an empty list rather than the whole workspace, and
// truncated reports that one of the search bounds (file count, bytes per
// file, result count) cut the result short, so the UI can say so instead
// of implying the list is complete.
func (s *Server) workspaceSymbols(w http.ResponseWriter, r *http.Request) {
	ws, err := s.workspaceWS()
	if err != nil {
		wsError(w, err)
		return
	}
	q := r.URL.Query().Get("q")
	syms, truncated, err := ws.SearchSymbols(q)
	if err != nil {
		wsError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"query":     q,
		"symbols":   syms,
		"truncated": truncated,
	})
}
