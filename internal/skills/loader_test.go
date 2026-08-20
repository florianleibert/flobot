package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pipelineBody = "# Pipeline\n\nTake a written product brief and drive it to merged code."

const pipelineSkill = `---
name: pipeline
description: Turn a product brief into an epic, break it into independent tasks, implement each behind review, and merge.
---

` + pipelineBody + `
`

func writeSkill(t *testing.T, dir, name, content string) {
	t.Helper()
	sub := filepath.Join(dir, name)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverFindsSkillFiles(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "pipeline", pipelineSkill)
	writeSkill(t, dir, "alpha", "# Alpha\n\nDo alpha things.")
	// A directory without SKILL.md is skipped.
	if err := os.MkdirAll(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A loose file is ignored.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	// Sorted by name.
	if got[0].Name != "alpha" || got[1].Name != "pipeline" {
		t.Fatalf("order = %q, %q", got[0].Name, got[1].Name)
	}
	if got[1].Description != "Turn a product brief into an epic, break it into independent tasks, implement each behind review, and merge." {
		t.Fatalf("description = %q", got[1].Description)
	}
	if got[1].Body != pipelineBody {
		t.Fatalf("body = %q, want %q", got[1].Body, pipelineBody)
	}
	if !strings.HasSuffix(got[1].Path, filepath.Join("pipeline", "SKILL.md")) {
		t.Fatalf("path = %q", got[1].Path)
	}
}

func TestDiscoverNoSkills(t *testing.T) {
	dir := t.TempDir()
	got, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

func TestDiscoverMissingDir(t *testing.T) {
	_, err := Discover(filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("want error for missing dir")
	}
}

func TestDiscoverNoFrontMatterUsesDirName(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "bare", "# Bare\n\nNo front matter here.")

	got, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name != "bare" {
		t.Fatalf("name = %q, want dir name", got[0].Name)
	}
	if got[0].Body != "# Bare\n\nNo front matter here." {
		t.Fatalf("body = %q", got[0].Body)
	}
}

func TestFind(t *testing.T) {
	skills := []Skill{{Name: "a"}, {Name: "b"}}
	if got := Find(skills, "b"); got == nil || got.Name != "b" {
		t.Fatalf("Find(b) = %+v", got)
	}
	if got := Find(skills, "c"); got != nil {
		t.Fatalf("Find(c) = %+v, want nil", got)
	}
}

func TestApplyPrependsBody(t *testing.T) {
	sk := &Skill{Body: pipelineBody}
	got := Apply(sk, "do the thing")
	want := pipelineBody + "\n\n---\n\n" + "do the thing"
	if got != want {
		t.Fatalf("Apply = %q, want %q", got, want)
	}
}

func TestApplyNilSkillUnmodified(t *testing.T) {
	prompt := "do the thing"
	if got := Apply(nil, prompt); got != prompt {
		t.Fatalf("Apply(nil) = %q, want %q", got, prompt)
	}
}

func TestApplyEmptyPrompt(t *testing.T) {
	sk := &Skill{Body: "body"}
	if got := Apply(sk, ""); got != "body\n\n---\n\n" {
		t.Fatalf("Apply = %q", got)
	}
}
