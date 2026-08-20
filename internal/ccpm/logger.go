// Package ccpm implements the CCPM pipeline run log.
//
// Every child run spawned through the pipeline (stages A-E) appends
// exactly one line to ./logs/ccpm.log in the format:
//
//	<timestamp> <pipeline_id> <stage A-E> <child_run_id> <status>
//
// Appends are made atomic with an exclusive advisory file lock (flock),
// and a child_run_id is logged at most once: a second Log call with the
// same child_run_id is a no-op.
package ccpm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LogRelPath is the default log location, relative to the working
// directory.
const LogRelPath = "logs/ccpm.log"

// Logger appends one line per child run to a CCPM log file. It is safe
// for concurrent use.
type Logger struct {
	path string

	mu       sync.Mutex
	seen     map[string]bool // child_run_ids already logged
	seenOnce bool            // whether the file was scanned for prior ids
}

// New returns a Logger writing to path. An empty path defaults to
// LogRelPath.
func New(path string) *Logger {
	if path == "" {
		path = LogRelPath
	}
	return &Logger{path: path, seen: make(map[string]bool)}
}

// Path returns the log file path.
func (l *Logger) Path() string { return l.path }

// Log appends one line for the given child run. stage must be one of
// A, B, C, D, E. If childRunID was already logged (either in this
// process or in a previous run of the log file), no line is appended
// and false is returned. On success true is returned.
func (l *Logger) Log(pipelineID, stage, childRunID, status string) (bool, error) {
	stage = strings.ToUpper(strings.TrimSpace(stage))
	if len(stage) != 1 || stage < "A" || stage > "E" {
		return false, fmt.Errorf("ccpm: invalid stage %q (want A-E)", stage)
	}
	childRunID = strings.TrimSpace(childRunID)
	if childRunID == "" {
		return false, fmt.Errorf("ccpm: empty child_run_id")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.seenOnce {
		l.loadSeen()
		l.seenOnce = true
	}
	if l.seen[childRunID] {
		return false, nil
	}

	line := fmt.Sprintf("%s %s %s %s %s\n",
		time.Now().UTC().Format(time.RFC3339),
		sanitizeField(pipelineID),
		stage,
		sanitizeField(childRunID),
		sanitizeField(status),
	)

	if err := l.appendLocked(line); err != nil {
		return false, err
	}
	l.seen[childRunID] = true
	return true, nil
}

// appendLocked opens the log file (creating the directory and file if
// needed), takes an exclusive advisory lock, appends line, and releases
// the lock.
func (l *Logger) appendLocked(line string) error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := lockFile(f); err != nil {
		return err
	}
	defer unlockFile(f)

	if _, err := f.WriteString(line); err != nil {
		return err
	}
	return f.Sync()
}

// loadSeen scans the existing log file and records every child_run_id
// already present, so duplicates are rejected across process restarts.
func (l *Logger) loadSeen() {
	f, err := os.Open(l.path)
	if err != nil {
		return
	}
	defer f.Close()

	_ = lockFile(f)
	defer unlockFile(f)

	buf := make([]byte, 64<<10)
	for {
		n, rErr := f.Read(buf)
		if n > 0 {
			for _, line := range strings.Split(string(buf[:n]), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 4 {
					l.seen[fields[3]] = true
				}
			}
		}
		if rErr != nil {
			return
		}
	}
}

// sanitizeField collapses whitespace so a value can never break the
// single-line, space-separated format.
func sanitizeField(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	return strings.Join(strings.Fields(s), " ")
}
