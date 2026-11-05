package goldenreport

import (
	"strings"
	"testing"
)

const diff = `--- a/internal/x/testdata/TestA.golden
+++ b/internal/x/testdata/TestA.golden
@@ -1,2 +1,2 @@
-old
+new
--- /dev/null
+++ b/internal/x/testdata/TestB.golden
@@ -0,0 +1 @@
+added
--- a/internal/x/testdata/TestC.golden
+++ /dev/null
@@ -1 +0,0 @@
-gone
`

func TestCleanResultIsOneLineWithMarker(t *testing.T) {
	got := Report(Results{}, "abc123", "")
	if !strings.Contains(got, Marker) || !strings.Contains(got, "no differences") || strings.Contains(got, "```") {
		t.Fatalf("report: %q", got)
	}
}

func TestReportHasHeaderCountsAndDiff(t *testing.T) {
	got := Report(Results{Diff: diff}, "abc123 against main", "")
	for _, want := range []string{
		Marker, "### Golden traces: abc123 against main",
		"3 golden(s) differ", "1 changed, 1 new, 1 removed",
		"`internal/x/testdata/TestA.golden` (changed)",
		"`internal/x/testdata/TestB.golden` (new)",
		"`internal/x/testdata/TestC.golden` (removed)",
		"```diff\n--- a/internal/x/testdata/TestA.golden", "-old\n+new",
		"does not fail the check",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report lacks %q:\n%s", want, got)
		}
	}
}

func TestStampFailureIsReportedEvenWithoutADiff(t *testing.T) {
	res := Results{StampFailed: true, StampLog: "FAIL internal/x TestA\n  boom\n"}
	if res.Clean() {
		t.Fatal("a failed stamp is not clean")
	}
	got := Report(res, "", "")
	if !strings.Contains(got, "Stamping failed") || !strings.Contains(got, "boom") {
		t.Fatalf("report: %s", got)
	}
}

func TestLongDiffIsCutAtALineAndLinksTheRun(t *testing.T) {
	long := Results{Diff: "--- a/f.golden\n+++ b/f.golden\n" + strings.Repeat("+line\n", 20_000)}
	got := Report(long, "", "https://example.com/run")
	if len(got) > 65_000 {
		t.Fatalf("comment is %d bytes", len(got))
	}
	if !strings.Contains(got, "cut short") || !strings.Contains(got, "https://example.com/run") {
		t.Fatalf("no note about the cut:\n%s", got[len(got)-300:])
	}
}

func TestFenceOutgrowsBackticksInTheDiff(t *testing.T) {
	got := fence("a ``` b", "diff")
	if !strings.HasPrefix(got, "````diff\n") || !strings.HasSuffix(got, "\n````") {
		t.Fatalf("fence: %q", got)
	}
}
