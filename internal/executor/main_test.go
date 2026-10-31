package executor

import (
	"os"
	"testing"
)

// TestMain isolates the package's tests from the real home directory.
//
// The executor writes its run log to ~/.flobot/ccpm-runs.md after every
// child. A test that spawns a child must not append to the operator's real
// run log, so HOME points at a temporary directory for the whole package.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "flobot-executor-test-home-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("HOME", dir); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
