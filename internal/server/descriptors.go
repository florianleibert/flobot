package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/florianleibert/flobot/internal/workspace"
)

// --- file descriptors (Code Studio) -----------------------------------------
//
// The editor opens a file when the user selects it and keeps the descriptor
// id. Reading and saving go through the descriptor, so a save always targets
// the file the user opened. A descriptor write is refused when the file
// changed on disk underneath it (optimistic concurrency): a flocode run or a
// second editor tab edited the file, and clobbering that work would lose it.
//
// Descriptors belong to the workspace view. A workspace switch closes them:
// they point into the old tree.

func (s *Server) workspaceDescriptors(w http.ResponseWriter, _ *http.Request) {
	ws, err := s.workspaceWS()
	if err != nil {
		wsError(w, err)
		return
	}
	writeJSON(w, map[string]any{"descriptors": ws.Descriptors()})
}

// workspaceDescriptorReleaseAll closes every open descriptor.
//
// A descriptor outlives the browser tab that opened it: the server keeps the
// handle until the client releases it, and a tab that closes without saying
// so leaks one. The registry is bounded, so a leak eventually exhausts it and
// the editor can no longer open a file. The editor calls this on page unload
// (with keepalive, so it survives the navigation).
func (s *Server) workspaceDescriptorReleaseAll(w http.ResponseWriter, _ *http.Request) {
	ws, err := s.workspaceWS()
	if err != nil {
		wsError(w, err)
		return
	}
	closed := len(ws.Descriptors())
	ws.CloseAllDescriptors()
	writeJSON(w, map[string]any{"ok": true, "closed": closed})
}

func (s *Server) workspaceDescriptorOpen(w http.ResponseWriter, r *http.Request) {
	ws, err := s.workspaceWS()
	if err != nil {
		wsError(w, err)
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	d, err := ws.Open(body.Path)
	if err != nil {
		wsError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, d)
}

func (s *Server) workspaceDescriptorGet(w http.ResponseWriter, r *http.Request) {
	ws, err := s.workspaceWS()
	if err != nil {
		wsError(w, err)
		return
	}
	id, ok := descriptorID(w, r)
	if !ok {
		return
	}
	f, err := ws.ReadDescriptor(id)
	if err != nil {
		wsError(w, err)
		return
	}
	writeJSON(w, f)
}

func (s *Server) workspaceDescriptorPut(w http.ResponseWriter, r *http.Request) {
	ws, err := s.workspaceWS()
	if err != nil {
		wsError(w, err)
		return
	}
	id, ok := descriptorID(w, r)
	if !ok {
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, workspace.MaxFileBytes+(4<<10), &body) {
		return
	}
	f, err := ws.WriteDescriptor(id, body.Content)
	if err != nil {
		wsError(w, err)
		return
	}
	writeJSON(w, f)
}

func (s *Server) workspaceDescriptorDelete(w http.ResponseWriter, r *http.Request) {
	ws, err := s.workspaceWS()
	if err != nil {
		wsError(w, err)
		return
	}
	id, ok := descriptorID(w, r)
	if !ok {
		return
	}
	if err := ws.CloseDescriptor(id); err != nil {
		wsError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// descriptorID parses the {id} path parameter.
func descriptorID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, `{"error":"invalid descriptor id"}`, http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

// descriptorError maps descriptor errors to HTTP responses. It reports
// whether it handled the error: when true, the response is already written
// and the caller must not write another one.
func descriptorError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, workspace.ErrUnknownDescriptor),
		errors.Is(err, workspace.ErrDescriptorClosed):
		http.Error(w, `{"error":"unknown file descriptor"}`, http.StatusNotFound)
		return true
	case errors.Is(err, workspace.ErrTooManyDescriptors):
		http.Error(w, `{"error":"too many open file descriptors"}`, http.StatusTooManyRequests)
		return true
	}
	return false
}
