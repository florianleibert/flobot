package store

import (
	"os"
	"testing"
)

// TestMain isolates test state from the real ~/.flobot/state.json. A test
// that saves without SetPath would otherwise overwrite the user's roster
// with seeded defaults.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "flobot-store-test-state-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("FLOBOT_TEST_STATE", dir); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
