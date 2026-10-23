package server

import "testing"

func TestParseJudge(t *testing.T) {
	cases := []struct {
		name  string
		out   string
		score float64
		why   string
	}{
		{"clean", "SCORE: 8\nWHY: Task completed with minor gaps.", 8, "Task completed with minor gaps."},
		{"lowercase", "score: 7\nwhy: done", 7, "done"},
		{"padded", "  SCORE:  9  \n  WHY:  full  ", 9, "full"},
		{"multiline why", "SCORE: 5\nWHY: first line\nsecond line", 5, "first line"},
		{"no score", "The run was good.", 0, ""},
		{"out of range", "SCORE: 12\nWHY: nope", 0, ""},
		{"zero", "SCORE: 0\nWHY: nope", 0, ""},
		{"decimal", "SCORE: 7.5\nWHY: mostly", 7.5, "mostly"},
		{"empty", "", 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, w := parseJudge(c.out)
			if s != c.score {
				t.Errorf("parseJudge(%q) score = %v, want %v", c.out, s, c.score)
			}
			if w != c.why {
				t.Errorf("parseJudge(%q) why = %q, want %q", c.out, w, c.why)
			}
		})
	}
}

func TestClip(t *testing.T) {
	if got := clip("hello", 10); got != "hello" {
		t.Errorf("clip short = %q", got)
	}
	if got := clip("abcdef", 4); got != "abc…" {
		t.Errorf("clip long = %q", got)
	}
}
