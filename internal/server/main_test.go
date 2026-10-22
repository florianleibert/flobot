package server

import (
	"os"
	"testing"
)

// TestMain isolates test state from the real ~/.flobot/state.json. Every
// test store that does not call SetPersistPath would otherwise persist
// bots/routines to the user's home — and enabled "every 1m" routines
// from tests would fire on a real server started later.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "flobot-server-test-state-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("FLOBOT_TEST_STATE", dir); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
