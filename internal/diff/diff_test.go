package diff

import (
	"strings"
	"testing"
)

const sampleDiff = `diff --git a/main.go b/main.go
index 111..222 100644
--- a/main.go
+++ b/main.go
@@ -10,4 +10,5 @@ func main() {
 ctx1
-old
+new
+extra
 ctx2
diff --git a/new.txt b/new.txt
new file mode 100644
index 000..333
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+hello
+world
diff --git a/gone.txt b/gone.txt
deleted file mode 100644
index 444..000
--- a/gone.txt
+++ /dev/null
@@ -1,1 +0,0 @@
-bye
`

func TestParse(t *testing.T) {
	fs := Parse(sampleDiff)
	if len(fs) != 3 {
		t.Fatalf("want 3 files, got %d", len(fs))
	}
	if fs[0].Path != "main.go" || len(fs[0].Hunks) != 1 {
		t.Errorf("file0: %+v", fs[0])
	}
	if fs[0].Additions != 2 || fs[0].Deletions != 1 {
		t.Errorf("counts: +%d -%d", fs[0].Additions, fs[0].Deletions)
	}
	if !fs[1].IsNew || fs[1].Path != "new.txt" {
		t.Errorf("file1: %+v", fs[1])
	}
	if !fs[2].IsDelete {
		t.Errorf("file2: %+v", fs[2])
	}
	tot := TotalsOf(fs)
	if tot.Files != 3 || tot.Additions != 4 || tot.Deletions != 2 {
		t.Errorf("totals: %+v", tot)
	}
}

func TestApplyExact(t *testing.T) {
	fs := Parse(sampleDiff)
	content := "a\nctx1\nold\nctx2\nz\n"
	res := Apply("main.go", content, true, fs[0], []int{0})
	if len(res.Skipped) != 0 {
		t.Fatalf("skipped: %v", res.Skipped)
	}
	want := "a\nctx1\nnew\nextra\nctx2\nz\n"
	if res.Content != want {
		t.Errorf("got %q want %q", res.Content, want)
	}
}

func TestApplyFuzzyTrailingDrift(t *testing.T) {
	fs := Parse(sampleDiff)
	// Trailing context drifted (ctx2 changed to ctx2b): exact fails,
	// context-trim fuzzy applies.
	content := "a\nctx1\nold\nctx2b\nz\n"
	res := Apply("main.go", content, true, fs[0], []int{0})
	if len(res.Skipped) != 0 {
		t.Fatalf("fuzzy failed: %v", res.Skipped)
	}
	if !strings.Contains(res.Content, "new") || !strings.Contains(res.Content, "extra") {
		t.Errorf("content: %q", res.Content)
	}
}

func TestApplyWhitespaceNormalized(t *testing.T) {
	fs := Parse(sampleDiff)
	// Whitespace drift on context lines.
	content := "a\n  ctx1\nold\n\tctx2\nz\n"
	res := Apply("main.go", content, true, fs[0], []int{0})
	if len(res.Skipped) != 0 {
		t.Fatalf("ws-normalized failed: %v", res.Skipped)
	}
}

func TestApplyUnappliableReported(t *testing.T) {
	fs := Parse(sampleDiff)
	res := Apply("main.go", "totally different\nfile\n", true, fs[0], []int{0})
	if len(res.Skipped) != 1 || res.Skipped[0] != 0 {
		t.Errorf("want hunk 0 skipped, got %+v", res)
	}
}

func TestApplyNewFile(t *testing.T) {
	fs := Parse(sampleDiff)
	res := Apply("new.txt", "", false, fs[1], []int{0})
	if res.Content != "hello\nworld\n" {
		t.Errorf("new file content: %q", res.Content)
	}
}

func TestApplyDeleteFile(t *testing.T) {
	fs := Parse(sampleDiff)
	res := Apply("gone.txt", "bye\n", true, fs[2], []int{0})
	if res.Exists {
		t.Errorf("delete should clear exists")
	}
}

func TestApplyOutOfRangeSkipped(t *testing.T) {
	fs := Parse(sampleDiff)
	res := Apply("main.go", "a\nctx1\nold\nctx2\nz\n", true, fs[0], []int{5})
	if len(res.Skipped) != 1 {
		t.Errorf("out of range: %+v", res)
	}
}

func TestApplyMultipleHunksReverseOrder(t *testing.T) {
	unified := `diff --git a/f b/f
--- a/f
+++ b/f
@@ -1,3 +1,3 @@
 one
-a1
+b1
 two
@@ -5,3 +5,3 @@
 five
-a2
+b2
 six
`
	fs := Parse(unified)
	content := "one\na1\ntwo\nthree\nfour\nfive\na2\nsix\n"
	res := Apply("f", content, true, fs[0], []int{0, 1})
	if len(res.Skipped) != 0 {
		t.Fatalf("skipped: %v", res.Skipped)
	}
	want := "one\nb1\ntwo\nthree\nfour\nfive\nb2\nsix\n"
	if res.Content != want {
		t.Errorf("got %q want %q", res.Content, want)
	}
}

func TestApplyNoNewlineMarkerNew(t *testing.T) {
	// Pre-image ends without newline; run adds a line and the new
	// file ends WITH one (no marker on the + side).
	unified := `diff --git a/f b/f
--- a/f
+++ b/f
@@ -1 +1,2 @@
-x
\ No newline at end of file
+x
+y
`
	fs := Parse(unified)
	res := Apply("f", "x", true, fs[0], []int{0})
	if res.Content != "x\ny\n" {
		t.Errorf("want %q got %q", "x\ny\n", res.Content)
	}
}

func TestApplyNoNewlineMarkerOld(t *testing.T) {
	// Pre-image ends with newline; run's last line ends without one.
	unified := `diff --git a/f b/f
--- a/f
+++ b/f
@@ -1 +1,2 @@
 x
+y
\ No newline at end of file
`
	fs := Parse(unified)
	res := Apply("f", "x\n", true, fs[0], []int{0})
	if res.Content != "x\ny" {
		t.Errorf("want %q got %q", "x\ny", res.Content)
	}
}

func TestApplyDuplicateIndicesAppliedOnce(t *testing.T) {
	unified := `diff --git a/f b/f
--- a/f
+++ b/f
@@ -1,3 +1,3 @@
 one
-a1
+b1
 two
`
	fs := Parse(unified)
	res := Apply("f", "one\na1\ntwo\n", true, fs[0], []int{0, 0})
	if len(res.Applied) != 1 || res.Applied[0] != 0 {
		t.Errorf("want hunk 0 applied once, got %+v", res)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("duplicate index must not be skipped, got %+v", res.Skipped)
	}
	if res.Content != "one\nb1\ntwo\n" {
		t.Errorf("content: %q", res.Content)
	}
}

func TestApplyPureAddOnNewlineOnlyFile(t *testing.T) {
	// Pre content is a single "\n": splits to ["",""], which the old
	// guard rejected. A pure-add hunk must apply.
	unified := `diff --git a/f b/f
--- a/f
+++ b/f
@@ -0,0 +1 @@
+added
`
	fs := Parse(unified)
	res := Apply("f", "\n", true, fs[0], []int{0})
	if len(res.Applied) != 1 {
		t.Fatalf("hunk not applied: %+v", res)
	}
	// The pre-image's lone empty line is replaced by the add, per the
	// empty-file semantics of the guard.
	if res.Content != "added\n" {
		t.Errorf("want %q got %q", "added\n", res.Content)
	}
}

func TestApplyFuzzyPreservesFileIndentation(t *testing.T) {
	// The file's ctx lines are indented; the diff's are not. Fuzzy
	// (whitespace-normalized) matching must not de-indent the file.
	unified := `diff --git a/f b/f
--- a/f
+++ b/f
@@ -1,3 +1,3 @@
 ctx1
-old
+new
 ctx2
`
	fs := Parse(unified)
	res := Apply("f", "\tctx1\nold\n\tctx2\n", true, fs[0], []int{0})
	if len(res.Applied) != 1 {
		t.Fatalf("hunk not applied: %+v", res)
	}
	if res.Content != "\tctx1\nnew\n\tctx2\n" {
		t.Errorf("indentation clobbered: %q", res.Content)
	}
}
