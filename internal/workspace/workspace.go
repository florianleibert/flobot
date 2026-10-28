// Package workspace provides safe, workspace-rooted file access for the
// Code Studio editor surface. All paths are workspace-relative; absolute
// paths, `..` segments, NUL bytes, and symlink escapes are rejected.
package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	// MaxFileBytes caps single-file reads and writes (2 MiB).
	MaxFileBytes = 2 << 20
	// MaxEntries caps directory listings.
	MaxEntries = 500
	// binarySniff is how many leading bytes are checked for NUL.
	binarySniff = 8 << 10
)

// ErrNotInWorkspace is returned when a resolved path escapes the root.
var ErrNotInWorkspace = errors.New("path escapes workspace root")

// ErrTooLarge is returned when a file exceeds MaxFileBytes.
var ErrTooLarge = errors.New("file exceeds 2 MiB cap")

// ErrBinary is returned (wrapped) when a file looks binary.
var ErrBinary = errors.New("binary file")

// ErrProtected is returned for paths the UI must never mutate (.git).
var ErrProtected = errors.New("path is protected")

// Workspace is a rooted file view.
type Workspace struct {
	root string
	// desc is the registry of open file descriptors (see descriptor.go).
	desc   descriptors
	descMu sync.Mutex
}

// New returns a Workspace rooted at dir (resolved to an absolute path).
func New(dir string) (*Workspace, error) {
	if dir == "" {
		return nil, fmt.Errorf("workspace: empty root")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("workspace: abs root: %w", err)
	}
	abs = filepath.Clean(abs)
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("workspace: stat root: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("workspace: root %s is not a directory", abs)
	}
	return &Workspace{root: abs}, nil
}

// Root returns the absolute workspace root.
func (w *Workspace) Root() string { return w.root }

// Name returns the base name of the root.
func (w *Workspace) Name() string { return filepath.Base(w.root) }

// validate checks a workspace-relative path and returns its absolute form.
// It rejects absolute paths, empty paths, NUL bytes, `..` segments, and
// symlink escapes. The result is guaranteed to stay under root.
func (w *Workspace) validate(rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("workspace: empty path")
	}
	if strings.ContainsRune(rel, 0) {
		return "", fmt.Errorf("workspace: NUL byte in path")
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") {
		return "", fmt.Errorf("workspace: absolute path %q", rel)
	}
	// Normalize separators and reject dot-dot anywhere.
	clean := filepath.ToSlash(rel)
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." {
			return "", fmt.Errorf("workspace: %q escapes root", rel)
		}
	}
	abs := filepath.Join(w.root, filepath.FromSlash(clean))
	// Symlink escape check: every existing component must resolve under root.
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		if !within(resolved, w.root) {
			return "", ErrNotInWorkspace
		}
		return resolved, nil
	}
	// Non-existent target: validate the deepest existing ancestor.
	// The walk uses filepath.Dir, not filepath.Split: Split returns the
	// same dir for a path with a trailing separator, so the walk stopped
	// at the first missing parent and every nested path was reported as
	// "unresolvable" ("a/b/c.js" with no "a" could never be created).
	dir, tail := filepath.Dir(abs), filepath.Base(abs)
	for {
		rd, err := filepath.EvalSymlinks(dir)
		if err == nil {
			if !within(rd, w.root) {
				return "", ErrNotInWorkspace
			}
			return filepath.Join(rd, tail), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("workspace: unresolvable path %q", rel)
		}
		tail = filepath.Join(filepath.Base(dir), tail)
		dir = parent
	}
}

func within(p, root string) bool {
	rp, err := filepath.EvalSymlinks(root)
	if err != nil {
		rp = root
	}
	return p == rp || strings.HasPrefix(p, rp+string(filepath.Separator))
}

// validateMutable is validate plus the .git protection rule for writes,
// deletes, and renames.
func (w *Workspace) validateMutable(rel string) (string, error) {
	if isProtectedRel(rel) {
		return "", ErrProtected
	}
	abs, err := w.validate(rel)
	if err != nil {
		return "", err
	}
	if w.isProtected(abs) {
		return "", ErrProtected
	}
	return abs, nil
}

// isProtectedRel rejects .git paths before any filesystem resolution, so
// a missing .git directory is still refused rather than 404-ing. The
// check is segment-wise and case-insensitive: on case-insensitive
// filesystems (macOS default) ".GIT/config" resolves to .git/config, and
// nested repos (sub/.git) are equally off-limits.
func isProtectedRel(rel string) bool {
	return hasGitSegment(filepath.ToSlash(filepath.Clean(rel)))
}

func hasGitSegment(slashPath string) bool {
	for _, seg := range strings.Split(slashPath, "/") {
		if strings.EqualFold(seg, ".git") {
			return true
		}
	}
	return false
}

func (w *Workspace) isProtected(abs string) bool {
	rel, err := filepath.Rel(w.root, abs)
	if err != nil {
		return true
	}
	return hasGitSegment(filepath.ToSlash(rel))
}

// Entry is one directory listing row.
type Entry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Type      string `json:"type"` // "dir" | "file"
	Size      int64  `json:"size,omitempty"`
	FileCount int    `json:"fileCount,omitempty"`
}

// ArtifactDirs are the dependency and build-output directories that the
// editor tree hides by default. They are listed only when the tree is
// requested with noise (GET /api/v1/workspace/tree?noise=1), which is
// what Code Studio's 👁 toggle does.
var ArtifactDirs = map[string]bool{
	"node_modules": true, "dist": true, "build": true, "out": true,
	"coverage": true, "webextensions": true, "target": true, "vendor": true,
	"bower_components": true, "venv": true, ".venv": true,
	"__pycache__": true, ".next": true, ".nuxt": true, ".vite": true,
	".cache": true, ".turbo": true, ".parcel-cache": true,
	".pytest_cache": true, ".mypy_cache": true, ".ruff_cache": true,
	".tox": true, ".terraform": true,
}

// Tree lists one directory level, sorted dirs-first then name. Dotfiles
// are skipped unless all is true; dependency/build artifact directories
// are skipped unless noise is true. The second result counts the artifact
// directories hidden at this level, so the UI can report them honestly.
func (w *Workspace) Tree(dir string, all, noise bool) ([]Entry, int, error) {
	abs, err := w.validate(dir)
	if err != nil {
		return nil, 0, err
	}
	dirents, err := os.ReadDir(abs)
	if err != nil {
		return nil, 0, err
	}
	entries := make([]Entry, 0, len(dirents))
	hidden := 0
	for _, de := range dirents {
		name := de.Name()
		if skipEntry(name, all, noise) {
			if ArtifactDirs[name] {
				hidden++
			}
			continue
		}
		// abs is symlink-resolved; compute rel against the resolved root
		// so paths stay workspace-relative even when root itself is a
		// symlink (e.g. /tmp -> /private/tmp on macOS).
		root := w.root
		if r, err := filepath.EvalSymlinks(w.root); err == nil {
			root = r
		}
		rel, err := filepath.Rel(root, filepath.Join(abs, name))
		if err != nil {
			continue
		}
		e := Entry{Name: name, Path: filepath.ToSlash(rel)}
		if de.IsDir() {
			e.Type = "dir"
			// Artifact trees can be huge (node_modules); don't walk them.
			if !ArtifactDirs[name] {
				e.FileCount = countFiles(filepath.Join(abs, name), 0)
			}
		} else {
			e.Type = "file"
			if fi, err := de.Info(); err == nil {
				e.Size = fi.Size()
			}
		}
		entries = append(entries, e)
		if len(entries) >= MaxEntries {
			break
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if (entries[i].Type == "dir") != (entries[j].Type == "dir") {
			return entries[i].Type == "dir"
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, hidden, nil
}

// skipEntry reports whether a tree entry is hidden. .git is always
// hidden: the editor may not touch it on any path. Dotfiles need all=1,
// artifact/build directories need noise=1.
func skipEntry(name string, all, noise bool) bool {
	if strings.EqualFold(name, ".git") {
		return true
	}
	if ArtifactDirs[name] {
		return !noise
	}
	if strings.HasPrefix(name, ".") {
		return !all
	}
	return false
}

// countFiles counts files under dir up to a small depth, for the tree UI.
func countFiles(dir string, depth int) int {
	if depth > 3 {
		return 0
	}
	dirents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, de := range dirents {
		if skipEntry(de.Name(), false, false) {
			continue
		}
		if de.IsDir() {
			n += countFiles(filepath.Join(dir, de.Name()), depth+1)
		} else {
			n++
		}
	}
	return n
}

// File is a read file result.
type File struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Content string `json:"content,omitempty"`
	SHA256  string `json:"sha256"`
	Binary  bool   `json:"binary,omitempty"`
	Exists  bool   `json:"exists"`
}

// Read returns file content (or a binary marker). Missing files return
// Exists=false, not an error, so the UI can distinguish.
func (w *Workspace) Read(rel string) (*File, error) {
	// .git is off-limits on the read path too: git config, reflogs,
	// and hooks routinely carry credentials and local history.
	if isProtectedRel(rel) {
		return nil, ErrProtected
	}
	abs, err := w.validate(rel)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &File{Path: filepath.ToSlash(rel), Exists: false}, nil
		}
		return nil, err
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("workspace: %q is a directory", rel)
	}
	if fi.Size() > MaxFileBytes {
		return nil, ErrTooLarge
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	f := &File{Path: filepath.ToSlash(rel), Size: fi.Size(), SHA256: hashBytes(raw), Exists: true}
	if isBinary(raw) {
		f.Binary = true
		return f, nil
	}
	f.Content = string(raw)
	return f, nil
}

func isBinary(raw []byte) bool {
	n := len(raw)
	if n > binarySniff {
		n = binarySniff
	}
	for i := 0; i < n; i++ {
		if raw[i] == 0 {
			return true
		}
	}
	return false
}

func hashBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ErrShaMismatch is the optimistic-concurrency conflict.
var ErrShaMismatch = errors.New("file changed since read (sha mismatch)")

// ErrExists is returned when creating a file that already exists.
var ErrExists = errors.New("file already exists")

// Write writes content atomically (temp file + rename). baseSha256, when
// non-empty, must match the current on-disk sha or the write is refused
// with ErrShaMismatch. An empty baseSha256 on an existing file is also a
// conflict — callers must read first.
func (w *Workspace) Write(rel, content, baseSha256 string) (*File, error) {
	abs, err := w.validateMutable(rel)
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > MaxFileBytes {
		return nil, ErrTooLarge
	}
	cur, err := os.ReadFile(abs)
	switch {
	case err == nil:
		if baseSha256 == "" {
			return nil, ErrShaMismatch
		}
		if hashBytes(cur) != baseSha256 {
			return nil, ErrShaMismatch
		}
	case errors.Is(err, fs.ErrNotExist):
		if baseSha256 != "" {
			return nil, ErrShaMismatch
		}
	default:
		return nil, err
	}
	if err := atomicWrite(abs, []byte(content)); err != nil {
		return nil, err
	}
	return &File{Path: filepath.ToSlash(rel), Size: int64(len(content)), SHA256: hashBytes([]byte(content)), Exists: true}, nil
}

// WriteForce overwrites rel with content unconditionally. It is the
// diff-apply path: the caller has just read the current content and
// applied hunks to it, so optimistic concurrency does not apply.
// The file's existing mode is preserved: a rebuilt 0755 script must
// not silently lose its executable bit to atomicWrite's 0644.
func (w *Workspace) WriteForce(rel, content string) (*File, error) {
	return w.WriteForceMode(rel, content, 0)
}

// WriteForceMode is WriteForce with an explicit file mode. mode 0
// preserves the existing file's mode (0644 for new files).
func (w *Workspace) WriteForceMode(rel, content string, mode os.FileMode) (*File, error) {
	if mode == 0 {
		if st, err := os.Lstat(filepath.Join(w.root, filepath.FromSlash(rel))); err == nil {
			mode = st.Mode().Perm()
		} else {
			mode = 0o644
		}
	}
	abs, err := w.validateMutable(rel)
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > MaxFileBytes {
		return nil, ErrTooLarge
	}
	if err := atomicWriteMode(abs, []byte(content), mode); err != nil {
		return nil, err
	}
	return &File{Path: filepath.ToSlash(rel), Size: int64(len(content)), SHA256: hashBytes([]byte(content)), Exists: true}, nil
}

// Create writes a new file; it refuses to overwrite an existing one.
func (w *Workspace) Create(rel, content string) (*File, error) {
	abs, err := w.validateMutable(rel)
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > MaxFileBytes {
		return nil, ErrTooLarge
	}
	if _, err := os.Lstat(abs); err == nil {
		return nil, ErrExists
	}
	if err := atomicWrite(abs, []byte(content)); err != nil {
		return nil, err
	}
	return &File{Path: filepath.ToSlash(rel), Size: int64(len(content)), SHA256: hashBytes([]byte(content)), Exists: true}, nil
}

// Delete removes a file. Missing files are a no-op success.
func (w *Workspace) Delete(rel string) error {
	abs, err := w.validateMutable(rel)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Rename moves from → to; refuses when to exists.
func (w *Workspace) Rename(from, to string) error {
	absFrom, err := w.validateMutable(from)
	if err != nil {
		return err
	}
	absTo, err := w.validateMutable(to)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(absTo); err == nil {
		return ErrExists
	}
	if _, err := os.Lstat(absFrom); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absTo), 0o755); err != nil {
		return err
	}
	return os.Rename(absFrom, absTo)
}

// atomicWrite writes via temp file + rename, mirroring state.json.
func atomicWrite(abs string, data []byte) error {
	return atomicWriteMode(abs, data, 0o644)
}

func atomicWriteMode(abs string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".flobot-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		return err
	}
	return os.Rename(name, abs)
}

// ReadAll returns the file content, or "" when missing/binary/oversize.
// Used by the diff-apply path, which tolerates missing files.
func (w *Workspace) ReadAll(rel string) (string, error) {
	f, err := w.Read(rel)
	if err != nil || f == nil || !f.Exists || f.Binary {
		return "", err
	}
	return f.Content, nil
}

var _ = io.Discard
