package main_test

import (
	"strings"
	"testing"

	"github.com/barry-hennessy/test/sweet"
)

func TestSnapshotIsStableAcrossRuns(t *testing.T) {
	sweet.Run(t, "snapshot of the same child twice is identical", wrapFactory, func(t *testing.T, d wrapDeps) {
		args := []string{"--snapshot=tree graph", "--", childBin, "-export-grpc-span", "-span-name", "child-work"}
		first := runPraytel(t, d.dir, args...)
		second := runPraytel(t, d.dir, args...)
		for _, res := range []procResult{first, second} {
			if res.exit != 0 {
				t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
			}
		}
		if first.stdout != second.stdout {
			t.Fatalf("snapshots differ\n--- first\n%s--- second\n%s", first.stdout, second.stdout)
		}
		if !strings.HasPrefix(first.stdout, "# praytel-snapshot v1\n## tree\n") || !strings.Contains(first.stdout, "child-work") {
			t.Fatalf("unexpected snapshot: %q", first.stdout)
		}
	})
}

func TestSnapshotFailedChildPrintsNothingAndReplaysOutput(t *testing.T) {
	sweet.Run(t, "snapshot of a failing child", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "--snapshot=tree", "--", childBin, "-exit", "7", "-stderr", "boom")
		if res.exit != 7 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
		if res.stdout != "" {
			t.Fatalf("stdout should be empty: %q", res.stdout)
		}
		if !strings.Contains(res.stderr, "boom") {
			t.Fatalf("child output not replayed: %q", res.stderr)
		}
	})
}

func TestSnapshotWithoutSpansIsIncomplete(t *testing.T) {
	sweet.Run(t, "snapshot of an uninstrumented child", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "--snapshot=tree", "--", "true")
		if res.exit != 3 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
		if res.stdout != "" || !strings.Contains(res.stderr, "no spans") {
			t.Fatalf("stdout=%q stderr=%q", res.stdout, res.stderr)
		}
	})
}
