package server

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/florianleibert/flobot/internal/workspace"
)

// workspaceWS returns the server's workspace, lazily initialized from
// workDir (or cwd when unset). A missing root is not fatal: handlers
// report the error; runs keep working (fail-closed review).
func (s *Server) workspaceWS() (*workspace.Workspace, error) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	if s.ws != nil {
		return s.ws, nil
	}
	dir := s.workDir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	ws, err := workspace.New(dir)
	if err != nil {
		return nil, err
	}
	s.ws = ws
	return ws, nil
}

// wsError maps workspace errors to HTTP responses.
func wsError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, workspace.ErrNotInWorkspace),
		errors.Is(err, workspace.ErrProtected):
		http.Error(w, `{"error":`+quoteJSON(err.Error())+"}", http.StatusBadRequest)
	case errors.Is(err, workspace.ErrShaMismatch):
		http.Error(w, `{"error":"file changed since read"}`, http.StatusConflict)
	case errors.Is(err, workspace.ErrExists):
		http.Error(w, `{"error":"file already exists"}`, http.StatusConflict)
	case errors.Is(err, workspace.ErrTooLarge):
		http.Error(w, `{"error":"file exceeds 2 MiB cap"}`, http.StatusRequestEntityTooLarge)
	case errors.Is(err, workspace.ErrBinary):
		http.Error(w, `{"error":"binary file"}`, http.StatusUnsupportedMediaType)
	case errors.Is(err, workspace.ErrNotInWorkspace):
		http.Error(w, `{"error":"path escapes workspace"}`, http.StatusBadRequest)
	case os.IsNotExist(err):
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	default:
		http.Error(w, `{"error":`+quoteJSON(err.Error())+"}", http.StatusBadRequest)
	}
}

func quoteJSON(s string) string {
	// Minimal JSON string quoting for error payloads.
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(" ")
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func (s *Server) workspaceRoot(w http.ResponseWriter, _ *http.Request) {
	ws, err := s.workspaceWS()
	if err != nil {
		wsError(w, err)
		return
	}
	writeJSON(w, map[string]any{"root": ws.Root(), "name": ws.Name()})
}

func (s *Server) workspaceTree(w http.ResponseWriter, r *http.Request) {
	ws, err := s.workspaceWS()
	if err != nil {
		wsError(w, err)
		return
	}
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = "."
	}
	all := r.URL.Query().Get("all") == "1"
	entries, err := ws.Tree(dir, all)
	if err != nil {
		wsError(w, err)
		return
	}
	writeJSON(w, map[string]any{"dir": dir, "entries": entries})
}

func (s *Server) workspaceFileGet(w http.ResponseWriter, r *http.Request) {
	ws, err := s.workspaceWS()
	if err != nil {
		wsError(w, err)
		return
	}
	f, err := ws.Read(r.URL.Query().Get("path"))
	if err != nil {
		wsError(w, err)
		return
	}
	if !f.Exists {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, f)
}

func (s *Server) workspaceFilePut(w http.ResponseWriter, r *http.Request) {
	ws, err := s.workspaceWS()
	if err != nil {
		wsError(w, err)
		return
	}
	var body struct {
		Content    string `json:"content"`
		BaseSHA256 string `json:"baseSha256"`
	}
	if !decodeJSON(w, r, workspace.MaxFileBytes+(4<<10), &body) {
		return
	}
	f, err := ws.Write(r.URL.Query().Get("path"), body.Content, body.BaseSHA256)
	if err != nil {
		wsError(w, err)
		return
	}
	writeJSON(w, f)
}

func (s *Server) workspaceFilePost(w http.ResponseWriter, r *http.Request) {
	ws, err := s.workspaceWS()
	if err != nil {
		wsError(w, err)
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, workspace.MaxFileBytes+(4<<10), &body) {
		return
	}
	f, err := ws.Create(r.URL.Query().Get("path"), body.Content)
	if err != nil {
		wsError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, f)
}

func (s *Server) workspaceFileDelete(w http.ResponseWriter, r *http.Request) {
	ws, err := s.workspaceWS()
	if err != nil {
		wsError(w, err)
		return
	}
	if err := ws.Delete(r.URL.Query().Get("path")); err != nil {
		wsError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) workspaceRename(w http.ResponseWriter, r *http.Request) {
	ws, err := s.workspaceWS()
	if err != nil {
		wsError(w, err)
		return
	}
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	if err := ws.Rename(body.From, body.To); err != nil {
		wsError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
