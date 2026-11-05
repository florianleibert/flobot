package skills

import (
	"strings"
	"testing"
)

func TestIndexIsOneLinePerSkillNoBodies(t *testing.T) {
	list := []Contract{
		{Name: "deploy", WhenToUse: "when shipping to production", Procedure: strings.Repeat("step\n", 200)},
		{Name: "audit", WhenToUse: "when reviewing a diff", Procedure: strings.Repeat("check\n", 200)},
	}
	out := Index(list)
	if strings.Count(out, "\n- ") != 2 && !strings.HasPrefix(out, "Skills") {
		t.Fatalf("index = %q", out)
	}
	if strings.Contains(out, "step") || strings.Contains(out, "check") {
		t.Fatalf("index leaked a procedure body: %q", out)
	}
	if !strings.Contains(out, "deploy") || !strings.Contains(out, "audit") {
		t.Fatalf("index missing names: %q", out)
	}
	if !strings.Contains(out, "when shipping to production") {
		t.Fatalf("index missing when-to-use: %q", out)
	}
}

func TestIndexIsStableAcrossOrder(t *testing.T) {
	a := []Contract{{Name: "zebra", WhenToUse: "z"}, {Name: "alpha", WhenToUse: "a"}}
	b := []Contract{{Name: "alpha", WhenToUse: "a"}, {Name: "zebra", WhenToUse: "z"}}
	if Index(a) != Index(b) {
		t.Fatalf("index is not order-stable:\n%q\n%q", Index(a), Index(b))
	}
}

func TestIndexEmpty(t *testing.T) {
	if got := Index(nil); got != "" {
		t.Fatalf("empty index = %q", got)
	}
}

func TestIndexLineFallsBackToBody(t *testing.T) {
	c := Contract{Name: "legacy", Body: "first line of the body\nsecond line"}
	if got := c.IndexLine(); !strings.Contains(got, "first line of the body") {
		t.Fatalf("IndexLine = %q", got)
	}
	if strings.Contains(c.IndexLine(), "second line") {
		t.Fatalf("IndexLine should be one line: %q", c.IndexLine())
	}
}

func TestRenderIncludesContractFields(t *testing.T) {
	c := Contract{
		Name:         "deploy",
		WhenToUse:    "when shipping",
		Procedure:    "1. build\n2. push",
		Pitfalls:     "do not skip the tag",
		Verification: "the tag exists on the remote",
	}
	out := c.Render()
	for _, want := range []string{"deploy", "when shipping", "1. build", "do not skip the tag", "the tag exists"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Render missing %q:\n%s", want, out)
		}
	}
}

func TestRenderLegacyBodyOnly(t *testing.T) {
	c := Contract{Name: "legacy", Body: "just do the thing"}
	if out := c.Render(); !strings.Contains(out, "just do the thing") {
		t.Fatalf("legacy Render = %q", out)
	}
}

func TestValidate(t *testing.T) {
	if err := Validate(Contract{}); err == nil {
		t.Fatal("empty contract must fail validation")
	}
	if err := Validate(Contract{Name: "x"}); err == nil {
		t.Fatal("contract with no when-to-use and no body must fail")
	}
	if err := Validate(Contract{Name: "x", Body: "b"}); err != nil {
		t.Fatalf("legacy contract should validate: %v", err)
	}
	if err := Validate(Contract{Name: "x", WhenToUse: "w"}); err != nil {
		t.Fatalf("full contract should validate: %v", err)
	}
}

func TestComplete(t *testing.T) {
	if (Contract{Name: "x"}).Complete() {
		t.Fatal("no when-to-use is not complete")
	}
	if !(Contract{Name: "x", WhenToUse: "w"}).Complete() {
		t.Fatal("name + when-to-use is complete")
	}
}
