package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// MaxDescriptors bounds the number of simultaneously open file descriptors
// per workspace. The editor opens a file when the user selects it and closes
// it when the tab goes away; the bound keeps a runaway client from pinning
// file state in memory.
const MaxDescriptors = 64

// ErrUnknownDescriptor is returned for a descriptor id that is not open.
var ErrUnknownDescriptor = errors.New("unknown file descriptor")

// ErrDescriptorClosed is returned when a closed descriptor is used.
var ErrDescriptorClosed = errors.New("file descriptor is closed")

// ErrTooManyDescriptors is returned when the open-descriptor limit is hit.
var ErrTooManyDescriptors = errors.New("too many open file descriptors")

// Descriptor is an open file handle: a stable id bound to one workspace
// path. The editor keeps the id across re-renders so a save always targets
// the file the user opened, not whatever the path happens to resolve to
// later.
//
// The binding is re-validated on every write, so swapping a symlink under
// the workspace cannot redirect a save outside the workspace root.
type Descriptor struct {
	ID      int64     `json:"id"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	SHA256  string    `json:"sha256"`
	ModTime time.Time `json:"mod_time"`
	Opened  time.Time `json:"opened"`
}

// descriptor is the internal handle behind a Descriptor.
type descriptor struct {
	Descriptor
	abs    string
	sha256 string
	closed bool
}

// descriptors is the per-workspace registry of open handles.
type descriptors struct {
	mu   sync.Mutex
	next int64
	open map[int64]*descriptor
}

func (d *descriptors) init() {
	if d.open == nil {
		d.open = make(map[int64]*descriptor)
	}
}

// Open opens rel as a file descriptor and returns its handle. The path must
// exist and be a regular file; .git paths are refused on every path, exactly
// as they are for reads and writes.
func (w *Workspace) Open(rel string) (*Descriptor, error) {
	// Check the descriptor budget before touching the filesystem: an
	// exhausted registry must fail fast rather than read a file it will
	// refuse to register.
	w.descMu.Lock()
	w.desc.init()
	full := len(w.desc.open) >= MaxDescriptors
	w.descMu.Unlock()
	if full {
		return nil, ErrTooManyDescriptors
	}

	abs, err := w.validateMutable(rel)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", fs.ErrNotExist, filepath.ToSlash(rel))
		}
		return nil, err
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("workspace: %q is a directory", filepath.ToSlash(rel))
	}
	if fi.Size() > MaxFileBytes {
		return nil, ErrTooLarge
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}

	w.descMu.Lock()
	defer w.descMu.Unlock()
	if len(w.desc.open) >= MaxDescriptors {
		return nil, ErrTooManyDescriptors
	}
	w.desc.next++
	d := &descriptor{
		Descriptor: Descriptor{
			ID:      w.desc.next,
			Path:    filepath.ToSlash(rel),
			Size:    fi.Size(),
			SHA256:  hashBytes(raw),
			ModTime: fi.ModTime(),
			Opened:  time.Now(),
		},
		abs:    abs,
		sha256: hashBytes(raw),
	}
	w.desc.open[d.ID] = d
	// Return a copy: the caller must not observe later mutations of the
	// live handle (the hash and size change on every write).
	out := d.Descriptor
	return &out, nil
}

// Descriptors returns the open handles, oldest id first.
func (w *Workspace) Descriptors() []Descriptor {
	w.descMu.Lock()
	defer w.descMu.Unlock()
	out := make([]Descriptor, 0, len(w.desc.open))
	for _, d := range w.desc.open {
		out = append(out, d.Descriptor)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Descriptor returns the handle for id.
func (w *Workspace) Descriptor(id int64) (*Descriptor, error) {
	d, err := w.descriptor(id)
	if err != nil {
		return nil, err
	}
	out := d.Descriptor
	return &out, nil
}

// ReadDescriptor reads the file behind an open descriptor and refreshes the
// descriptor's observed size, hash and mtime.
func (w *Workspace) ReadDescriptor(id int64) (*File, error) {
	d, err := w.descriptor(id)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(d.abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", fs.ErrNotExist, d.Path)
		}
		return nil, err
	}
	if int64(len(raw)) > MaxFileBytes {
		return nil, ErrTooLarge
	}
	f := &File{Path: d.Path, Size: int64(len(raw)), SHA256: hashBytes(raw), Exists: true}
	if isBinary(raw) {
		f.Binary = true
	} else {
		f.Content = string(raw)
	}

	w.descMu.Lock()
	d.Size = f.Size
	d.sha256 = f.SHA256
	d.SHA256 = f.SHA256
	if fi, err := os.Stat(d.abs); err == nil {
		d.ModTime = fi.ModTime()
	}
	w.descMu.Unlock()
	return f, nil
}

// WriteDescriptor writes content to the file behind an open descriptor.
//
// The write is refused with ErrShaMismatch when the file changed on disk
// since the descriptor last read it: another editor (or a flocode run)
// touched the file, and silently overwriting that work would lose it.
// The descriptor's own writes update the tracked hash, so repeated saves
// through one descriptor are not conflicts.
func (w *Workspace) WriteDescriptor(id int64, content string) (*File, error) {
	d, err := w.descriptor(id)
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > MaxFileBytes {
		return nil, ErrTooLarge
	}
	// Re-validate: the path must still be inside the workspace and must not
	// have become a protected (.git) path since the descriptor was opened.
	abs, err := w.validateMutable(d.Path)
	if err != nil {
		return nil, err
	}
	cur, err := os.ReadFile(abs)
	switch {
	case err == nil:
		if hashBytes(cur) != w.descriptorSHA(d) {
			return nil, ErrShaMismatch
		}
	case errors.Is(err, fs.ErrNotExist):
		// The file was deleted under the descriptor. Recreating it is the
		// user's intent when they hit save on an open tab.
	default:
		return nil, err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Lstat(abs); err == nil {
		mode = st.Mode().Perm()
	}
	if err := atomicWriteMode(abs, []byte(content), mode); err != nil {
		return nil, err
	}
	f := &File{Path: d.Path, Size: int64(len(content)), SHA256: hashBytes([]byte(content)), Exists: true}

	w.descMu.Lock()
	d.abs = abs
	d.Size = f.Size
	d.sha256 = f.SHA256
	d.SHA256 = f.SHA256
	d.ModTime = time.Now()
	w.descMu.Unlock()
	return f, nil
}

// CloseDescriptor releases a descriptor. Closing an unknown descriptor is an
// error; closing an already-closed one is not.
func (w *Workspace) CloseDescriptor(id int64) error {
	w.descMu.Lock()
	defer w.descMu.Unlock()
	d, ok := w.desc.open[id]
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownDescriptor, id)
	}
	d.closed = true
	delete(w.desc.open, id)
	return nil
}

// CloseAllDescriptors releases every open descriptor. It is called when the
// workspace is swapped: descriptors point into the old tree and must not
// outlive it.
func (w *Workspace) CloseAllDescriptors() {
	w.descMu.Lock()
	defer w.descMu.Unlock()
	for id, d := range w.desc.open {
		d.closed = true
		delete(w.desc.open, id)
	}
}

// descriptor resolves an id to its handle.
func (w *Workspace) descriptor(id int64) (*descriptor, error) {
	w.descMu.Lock()
	defer w.descMu.Unlock()
	d, ok := w.desc.open[id]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownDescriptor, id)
	}
	if d.closed {
		return nil, ErrDescriptorClosed
	}
	return d, nil
}

// descriptorSHA reads the tracked content hash under the lock.
func (w *Workspace) descriptorSHA(d *descriptor) string {
	w.descMu.Lock()
	defer w.descMu.Unlock()
	return d.sha256
}
