package store

import (
	"path/filepath"
	"testing"
)

func TestRunStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rs := NewRunStore(filepath.Join(dir, "runs.json"))
	if err := rs.Record(RunSummary{ID: "run-1", BotID: "bot-1", Status: "done", ReviewState: "pending"}); err != nil {
		t.Fatal(err)
	}
	if err := rs.Record(RunSummary{ID: "run-2", BotID: "bot-1", Status: "done", ReviewState: "pending",
		DiffSummary: &DiffStat{Files: 3, Additions: 41, Deletions: 12}}); err != nil {
		t.Fatal(err)
	}
	// Reload from disk.
	rs2 := NewRunStore(filepath.Join(dir, "runs.json"))
	if err := rs2.Load(); err != nil {
		t.Fatal(err)
	}
	got, ok := rs2.Get("run-2")
	if !ok || got.DiffSummary == nil || got.DiffSummary.Additions != 41 {
		t.Fatalf("run-2: %+v", got)
	}
	if err := rs2.SetReviewState("run-2", "accepted"); err != nil {
		t.Fatal(err)
	}
	rs3 := NewRunStore(filepath.Join(dir, "runs.json"))
	_ = rs3.Load()
	g2, _ := rs3.Get("run-2")
	if g2.ReviewState != "accepted" {
		t.Errorf("review state: %q", g2.ReviewState)
	}
	list := rs3.List()
	if len(list) != 2 || list[0].ID != "run-2" {
		t.Errorf("list order: %+v", list)
	}
}

func TestRunStoreLeakCheck(t *testing.T) {
	dir := t.TempDir()
	rs := NewRunStore(filepath.Join(dir, "runs.json"))
	err := rs.Record(RunSummary{ID: "run-x", Status: "done", ReviewState: "pending", BotID: "api_key"})
	if err == nil {
		t.Error("expected leak-check failure for api_key in botId")
	}
}
