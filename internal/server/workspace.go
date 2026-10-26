package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/florianleibert/flobot/internal/workspace"
)

// workspaceWS returns the server's workspace, lazily initialized from
// workDir (or cwd when unset). A missing root is not fatal: handlers
// report the error; runs keep working (fail-closed review).
func (s *Server) workspaceWS() (*workspace.Workspace, error) {
	// Read workDir before taking wsMu: workspaceSelect takes workMu then
	// wsMu while swapping folders at runtime, so taking wsMu here and then
	// blocking on workDirNow() would be an AB-BA inversion with a concurrent
	// folder-picker switch.
	dir := s.workDirNow()
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	if s.ws != nil {
		return s.ws, nil
	}
	return s.openWorkspaceLocked(dir)
}

// openWorkspaceLocked builds and stores the workspace view. wsMu must be
// held. An empty dir falls back to the process cwd.
func (s *Server) openWorkspaceLocked(dir string) (*workspace.Workspace, error) {
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
	// noise=1 reveals dependency/build artifact directories (node_modules,
	// dist, …). They stay hidden by default; the Code Studio 👁 toggle asks
	// for them explicitly. hidden reports how many were left out at this
	// level so the UI can say so honestly.
	noise := r.URL.Query().Get("noise") == "1"
	entries, hidden, err := ws.Tree(dir, all, noise)
	if err != nil {
		wsError(w, err)
		return
	}
	writeJSON(w, map[string]any{"dir": dir, "entries": entries, "hidden": hidden})
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

// --- workspace selection (Code Studio folder picker) ------------------------
//
// The desktop GUI launches `flobot serve` with a default workspace
// (~/flobot-workspace). The Code panel can switch to any existing folder at
// runtime: POST /api/v1/workspace/select swaps workDir + the workspace view
// so the editor tree, file I/O, run cwd and checkpoints all follow. The
// choice is persisted to ~/.flobot/config.json (recents, newest first) so
// the next launch reopens the same folder.

const maxRecentWorkspaces = 8

// workspaceConfig is the persisted UI-facing server config.
type workspaceConfig struct {
	Workspaces []string `json:"workspaces"`
}

func (s *Server) workspaceConfigPath() string {
	home := s.home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".flobot", "config.json")
}

// LatestWorkspace returns the most recently selected workspace folder from
// ~/.flobot/config.json, or "" when unset or the folder no longer exists.
// Used by `flobot serve` so a runtime folder choice survives a relaunch.
func LatestWorkspace() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, ".flobot", "config.json"))
	if err != nil {
		return ""
	}
	var cfg workspaceConfig
	if json.Unmarshal(b, &cfg) != nil || len(cfg.Workspaces) == 0 {
		return ""
	}
	dir := cfg.Workspaces[0]
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return ""
	}
	return dir
}

func (s *Server) loadWorkspaceConfig() workspaceConfig {
	var cfg workspaceConfig
	path := s.workspaceConfigPath()
	if path == "" {
		return cfg
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(b, &cfg)
	return cfg
}

// rememberWorkspace moves dir to the front of the recents list and persists
// it. Best-effort: a write failure must not fail the switch itself.
func (s *Server) rememberWorkspace(dir string) {
	path := s.workspaceConfigPath()
	if path == "" {
		return
	}
	cfg := s.loadWorkspaceConfig()
	kept := make([]string, 0, len(cfg.Workspaces)+1)
	kept = append(kept, dir)
	for _, w := range cfg.Workspaces {
		if w != dir && len(kept) < maxRecentWorkspaces {
			kept = append(kept, w)
		}
	}
	cfg.Workspaces = kept
	if b, err := json.MarshalIndent(cfg, "", "  "); err == nil {
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr == nil {
			_ = os.WriteFile(path, b, 0o644)
		}
	}
}

// isSafeWorkspace rejects switching to the filesystem root or a bare home
// directory: those were the original "editor shows the whole disk" bug.
func isSafeWorkspace(abs string) bool {
	if abs == "/" || abs == "" {
		return false
	}
	if home, err := os.UserHomeDir(); err == nil && abs == home {
		return false
	}
	return true
}

func (s *Server) workspaceRoots(w http.ResponseWriter, _ *http.Request) {
	s.workMu.RLock()
	current := s.workDir
	s.workMu.RUnlock()
	writeJSON(w, map[string]any{"current": current, "recent": s.loadWorkspaceConfig().Workspaces})
}

func (s *Server) workspaceSelect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, 1<<16, &body) {
		return
	}
	effectivePath := func(p string) string {
		if ep, err := filepath.EvalSymlinks(p); err == nil {
			return ep
		}
		return p
	}
	raw := strings.TrimSpace(body.Path)
	if raw == "" {
		http.Error(w, `{"error":"path required"}`, http.StatusBadRequest)
		return
	}
	if !filepath.IsAbs(raw) {
		http.Error(w, `{"error":"absolute path required"}`, http.StatusBadRequest)
		return
	}
	abs := filepath.Clean(raw)
	if !isSafeWorkspace(abs) || !isSafeWorkspace(effectivePath(abs)) {
		http.Error(w, `{"error":"refusing to open / or your home directory as a workspace; pick a project folder"}`, http.StatusBadRequest)
		return
	}
	// Creating the target is only allowed inside ~/flobot-workspace (a new
	// project subfolder); anywhere else it must already exist.
	st, err := os.Stat(abs)
	if err != nil {
		defRoot := filepath.Join(homeOrCwd(), "flobot-workspace")
		if !strings.HasPrefix(abs+string(os.PathSeparator), effectivePath(defRoot)+string(os.PathSeparator)) {
			http.Error(w, `{"error":"folder does not exist"}`, http.StatusNotFound)
			return
		}
		if mkErr := os.MkdirAll(abs, 0o755); mkErr != nil {
			http.Error(w, `{"error":`+quoteJSON(mkErr.Error())+"}", http.StatusBadRequest)
			return
		}
		st, err = os.Stat(abs)
		if err != nil {
			http.Error(w, `{"error":`+quoteJSON(err.Error())+"}", http.StatusBadRequest)
			return
		}
	}
	if !st.IsDir() {
		http.Error(w, `{"error":"not a directory"}`, http.StatusBadRequest)
		return
	}

	// A run spawns flocode with cwd = this folder. flocode refuses to start
	// in a non-git folder (it prompts, then dies on the non-interactive
	// child stdin), and Code Studio checkpoints are git-based, so a selected
	// workspace must be a repo. Initialise one when the folder is plain —
	// best-effort: a folder without git still browses/edits, only runs and
	// review are degraded. Done before the swap (and outside revMu: git can
	// take seconds, and revMu serializes diff-review apply/reject).
	if !isGitRepo(abs) {
		_ = ensureGitRepo(abs)
	}

	// Swap under both locks: validate the view first so a rejected path
	// leaves the server exactly as it was. revMu is the same lock apply
	// and reject take, so a workspace switch cannot land mid-review.
	// (Checkpoint stores are stateless wrappers over (root, dir), so
	// s.cp is re-read lazily and needs no invalidation.)
	s.revMu.Lock()
	defer s.revMu.Unlock()
	s.workMu.Lock()
	s.wsMu.Lock()
	ws, err := s.openWorkspaceLocked(abs)
	if err != nil {
		s.wsMu.Unlock()
		s.workMu.Unlock()
		http.Error(w, `{"error":`+quoteJSON(err.Error())+"}", http.StatusBadRequest)
		return
	}
	s.workDir = abs
	s.wsMu.Unlock()
	s.workMu.Unlock()

	s.rememberWorkspace(abs)
	writeJSON(w, map[string]any{"root": ws.Root(), "name": ws.Name()})
}

func homeOrCwd() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	cwd, _ := os.Getwd()
	return cwd
}

// isGitRepo reports whether dir is inside a git working tree.
func isGitRepo(dir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// ensureGitRepo runs `git init` in dir. Best-effort: the caller degrades cleanly
// when git is missing or the folder is not writable.
func ensureGitRepo(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "git", "init", "-q", dir).Run()
}

// runFailureHint turns a cryptic child-exit error into a one-line cause when
// the failure is one of flocode's known fatal modes. The child's stream-json
// carries no auth/git errors (they abort the process or print human text), so
// the captured stderr ring is the only place the cause appears. Returns "" for
// anything unrecognized, leaving the raw error intact. Only the diagnostic
// tail is echoed; it is already key-scrubbed by the executor.
func runFailureHint(errText, stderr string) string {
	lowErr := strings.ToLower(errText)
	low := strings.ToLower(stderr)
	if !strings.Contains(lowErr, "exit status") && !strings.Contains(lowErr, "signal") {
		return ""
	}
	switch {
	case strings.Contains(low, "http 401") || strings.Contains(low, "401 unauthorized"):
		return "model rejected the API key (HTTP 401) — the route's key is expired or invalid; refresh ~/.accounts/meta.ai"
	case strings.Contains(low, "is not set"):
		return "route needs MODEL_API_KEY but it was not provided — set ~/.accounts/meta.ai for a Muse route"
	case strings.Contains(low, "not a git repository"):
		return "workspace is not a git repository — pick a folder that is, or let the folder picker initialise one"
	case strings.Contains(low, "no_equivalent_route") || strings.Contains(low, "connection refused"):
		return "no model is reachable for this route (backend down or no equivalent route) — start the model server or switch routes"
	}
	if tail := strings.TrimSpace(stderr); tail != "" {
		if len(tail) > 400 {
			tail = "…" + tail[len(tail)-400:]
		}
		// Collapse newlines so the hint stays a single run-detail line.
		return "stderr: " + strings.Join(strings.Fields(tail), " ")
	}
	return ""
}
