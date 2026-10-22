// Package checkpoint captures attribution-correct git snapshots of the
// workspace before and after each run, and restores them on demand.
//
// A snapshot is a git tree object written via a temporary index
// (GIT_INDEX_FILE + add -A + write-tree), so the user's index and
// worktree are never touched. The run's diff is diff-tree pre post,
// which correctly attributes only run-caused changes even on a dirty
// tree, and includes untracked files.
package checkpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNotGitRepo is returned when the workspace is not a git repository.
// Runs still work; review is unavailable (fail-closed).
var ErrNotGitRepo = errors.New("workspace is not a git repo")

// Snapshot is a captured tree.
type Snapshot struct {
	Tree      string // git tree sha
	CreatedAt time.Time
}

// Checkpoint is a restorable capture of workspace state.
type Checkpoint struct {
	ID        string `json:"id"`
	RunID     string `json:"runId,omitempty"`
	Tree      string `json:"tree"`
	Parent    string `json:"parent,omitempty"` // tree this checkpoint restores from
	CreatedAt string `json:"createdAt"`
	Files     int    `json:"files"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// Store captures and restores git snapshots for one workspace root.
type Store struct {
	root string
	// dir holds checkpoint metadata (runs.json lives beside it in ~/.flobot).
	dir string
	// mu serializes metadata read-modify-write cycles: concurrent
	// Save calls (post-run capture, apply safety, restore safety) would
	// otherwise each load the same list and drop each other's appends.
	mu sync.Mutex
}

// New returns a Store over root with metadata under dir.
func New(root, dir string) (*Store, error) {
	if root == "" || dir == "" {
		return nil, fmt.Errorf("checkpoint: root and dir required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{root: root, dir: dir}, nil
}

// git runs a git command in the workspace root.
func (s *Store) git(ctx context.Context, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = s.root
	if len(env) > 0 {
		e := os.Environ()
		e = append(e, env...)
		cmd.Env = e
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// IsRepo reports whether the workspace root is inside a git work tree.
func (s *Store) IsRepo(ctx context.Context) bool {
	_, err := s.git(ctx, nil, "rev-parse", "--git-dir")
	return err == nil
}

// Capture writes a temp-index tree of the current workspace state
// (tracked + untracked, .gitignored files excluded via add -A semantics).
// It never touches the user's index or worktree.
func (s *Store) Capture(ctx context.Context) (Snapshot, error) {
	if !s.IsRepo(ctx) {
		return Snapshot{}, ErrNotGitRepo
	}
	tmp, err := os.CreateTemp("", "flobot-index-*")
	if err != nil {
		return Snapshot{}, err
	}
	idxPath := tmp.Name()
	tmp.Close()
	defer os.Remove(idxPath)

	env := []string{"GIT_INDEX_FILE=" + idxPath}
	defer os.Remove(idxPath + ".lock") // git's lock file if a capture is killed mid-add
	// read-tree seeds the temp index from HEAD so unchanged files do not
	// need re-hashing; add -A layers the worktree state (incl. untracked).
	// A repo with zero commits has no HEAD: an empty index is correct.
	if _, err := s.git(ctx, env, "read-tree", "HEAD"); err != nil {
		if _, err2 := s.git(ctx, nil, "rev-parse", "--verify", "-q", "HEAD"); err2 != nil {
			// unborn HEAD: start from an empty index
			if err := os.WriteFile(idxPath, nil, 0o600); err != nil {
				return Snapshot{}, err
			}
		} else {
			return Snapshot{}, err
		}
	}
	if _, err := s.git(ctx, env, "add", "-A"); err != nil {
		return Snapshot{}, err
	}
	out, err := s.git(ctx, env, "write-tree")
	if err != nil {
		return Snapshot{}, err
	}
	tree := strings.TrimSpace(out)
	if tree == "" {
		return Snapshot{}, fmt.Errorf("checkpoint: empty tree")
	}
	return Snapshot{Tree: tree, CreatedAt: time.Now()}, nil
}

// Diff returns the unified diff between two snapshots.
func (s *Store) Diff(ctx context.Context, pre, post string) (string, error) {
	out, err := s.git(ctx, nil, "diff-tree", "-r", "-p", "--no-color", pre, post)
	if err != nil {
		return "", err
	}
	return out, nil
}

// ReadFile returns a file's content from a tree. ok is false when the
// path does not exist in that tree.
func (s *Store) ReadFile(ctx context.Context, tree, path string) (content string, ok bool, err error) {
	if path == "" {
		return "", false, fmt.Errorf("checkpoint: empty path")
	}
	// ls-tree tells existence without failing on missing paths. The
	// :(literal) magic disables pathspec globbing so a file literally
	// named "*.go" is matched exactly, not as a wildcard.
	out, err := s.git(ctx, nil, "ls-tree", "-z", tree, "--", ":(literal)"+path)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(strings.TrimRight(out, "\x00")) == "" {
		return "", false, nil
	}
	body, err := s.git(ctx, nil, "show", tree+":"+path)
	if err != nil {
		return "", false, err
	}
	return body, true, nil
}

// FileMode returns the permission bits a path has in a tree (e.g.
// 0o755), or 0 when the path is absent. Used by diff-apply to restore
// a reverted file's pre-run mode, not just its content.
func (s *Store) FileMode(ctx context.Context, tree, path string) (os.FileMode, error) {
	if path == "" {
		return 0, nil
	}
	out, err := s.git(ctx, nil, "ls-tree", tree, "--", path)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(out)
	if len(fields) < 1 {
		return 0, nil
	}
	m, err := strconv.ParseUint(fields[0], 8, 32)
	if err != nil {
		return 0, nil
	}
	return os.FileMode(m).Perm(), nil
}

// isHexOID reports whether s is a full 40-char lowercase hex object id.
func isHexOID(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// Save records a checkpoint (metadata only; the tree object lives in git's
// object database, which is content-addressed and never GC'd while referenced).
func (s *Store) Save(cp Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cps, err := s.load()
	if err != nil {
		return err
	}
	// Dedup: an identical consecutive checkpoint (same tree and run)
	// is common — a review's safety checkpoint often equals the pre-run
	// one. Keep one.
	if n := len(cps); n > 0 {
		last := cps[n-1]
		if last.Tree == cp.Tree && last.RunID == cp.RunID {
			return nil
		}
	}
	cps = append(cps, cp)
	if len(cps) > 200 {
		cps = cps[len(cps)-200:]
	}
	return s.save(cps)
}

// List returns checkpoints, newest first.
func (s *Store) List() ([]Checkpoint, error) {
	cps, err := s.load()
	if err != nil {
		return nil, err
	}
	// newest first
	for i, j := 0, len(cps)-1; i < j; i, j = i+1, j-1 {
		cps[i], cps[j] = cps[j], cps[i]
	}
	return cps, nil
}

// Get returns one checkpoint by id.
func (s *Store) Get(id string) (Checkpoint, error) {
	cps, err := s.load()
	if err != nil {
		return Checkpoint{}, err
	}
	for _, cp := range cps {
		if cp.ID == id {
			return cp, nil
		}
	}
	return Checkpoint{}, os.ErrNotExist
}

// RestoreTree checks out an arbitrary tree (e.g. a pre-run snapshot)
// into the worktree, capturing the current state as a safety
// checkpoint first. Untracked files created after that tree (e.g. by
// a run being rejected) are removed, like Restore.
func (s *Store) RestoreTree(ctx context.Context, tree string) (Checkpoint, error) {
	// tree comes from runs.json / in-memory state; validate it is a
	// full hex object id so a corrupted field cannot smuggle flags
	// (e.g. "--upload-pack") into git.
	if !isHexOID(tree) {
		return Checkpoint{}, fmt.Errorf("invalid tree id %q", tree)
	}
	cur, err := s.Capture(ctx)
	if err != nil {
		return Checkpoint{}, err
	}
	safety := Checkpoint{
		ID:        newID("cp"),
		Tree:      cur.Tree,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.Save(safety); err != nil {
		return Checkpoint{}, err
	}
	if _, err := s.git(ctx, nil, "read-tree", "--reset", "-u", tree); err != nil {
		return Checkpoint{}, err
	}
	// read-tree -u only removes files present in the user's index; a
	// run-created file was never there. clean -fd removes it.
	cleanArgs := []string{"clean", "-fd"}
	if inside, _ := isSubPath(s.root, s.dir); inside {
		cleanArgs = append(cleanArgs, "-e", filepath.Base(s.dir))
	}
	if _, err := s.git(ctx, nil, cleanArgs...); err != nil {
		return Checkpoint{}, err
	}
	return safety, nil
}

// Restore checks out a checkpoint's tree into the worktree. It captures
// the current state as a new checkpoint first, so a restore is itself
// reversible. The checkout is forced (the checkpoint is the authority).
func (s *Store) Restore(ctx context.Context, id string) (Checkpoint, error) {
	cp, err := s.Get(id)
	if err != nil {
		return Checkpoint{}, err
	}
	// Safety checkpoint of current state.
	cur, err := s.Capture(ctx)
	if err != nil {
		return Checkpoint{}, err
	}
	safety := Checkpoint{
		ID:        newID("cp"),
		Tree:      cur.Tree,
		Parent:    cp.ID,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.Save(safety); err != nil {
		return Checkpoint{}, err
	}
	// Restore: reset index+worktree to the checkpoint tree, then remove
	// files untracked relative to it (the checkpoint is the authority).
	if _, err := s.git(ctx, nil, "read-tree", "--reset", "-u", cp.Tree); err != nil {
		return Checkpoint{}, err
	}
	// The metadata dir is normally outside the repo (~/.flobot); when it
	// is inside (tests, custom setups) it must survive the clean.
	cleanArgs := []string{"clean", "-fd"}
	if inside, _ := isSubPath(s.root, s.dir); inside {
		cleanArgs = append(cleanArgs, "-e", filepath.Base(s.dir))
	}
	if _, err := s.git(ctx, nil, cleanArgs...); err != nil {
		return Checkpoint{}, err
	}
	return safety, nil
}

// isSubPath reports whether path is inside dir.
func isSubPath(dir, path string) (bool, error) {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false, err
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

func (s *Store) metaPath() string { return filepath.Join(s.dir, "checkpoints.json") }

func (s *Store) load() ([]Checkpoint, error) {
	raw, err := os.ReadFile(s.metaPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var cps []Checkpoint
	if err := jsonUnmarshal(raw, &cps); err != nil {
		return nil, err
	}
	return cps, nil
}

func (s *Store) save(cps []Checkpoint) error {
	raw, err := jsonMarshalIndent(cps)
	if err != nil {
		return err
	}
	tmp := s.metaPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.metaPath())
}

func newID(prefix string) string {
	return fmt.Sprintf("%s-%x", prefix, time.Now().UnixNano())
}

// jsonUnmarshal and jsonMarshalIndent are thin wrappers so this package
// does not import encoding/json at every call site.
func jsonUnmarshal(raw []byte, v any) error   { return json.Unmarshal(raw, v) }
func jsonMarshalIndent(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }
