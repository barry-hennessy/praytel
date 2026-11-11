package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/barry-hennessy/test/sweet"
)

func TestExecuteFansOutToTwoOutputs(t *testing.T) {
	sweet.Run(t, "a snapshot to a file and the stream to stdout in one run", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir,
			"-e", `snapshot("tree") | final | file("tree.txt") | exit`,
			"--execute", `stream() | each | stdout`,
			"--", childBin, "-export-grpc-span", "-span-name", "child-work")
		if res.exit != 0 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
		tree, err := os.ReadFile(filepath.Join(d.dir, "tree.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(tree), "# praytel-snapshot v1\n## tree\n") || !strings.Contains(string(tree), "child-work") {
			t.Fatalf("tree.txt = %q", tree)
		}
		if !strings.Contains(res.stdout, "stdout child-work") {
			t.Fatalf("stream did not print the joined log: %q", res.stdout)
		}
	})
}

func TestExecuteFile(t *testing.T) {
	sweet.Run(t, "a .star file declares the outputs", wrapFactory, func(t *testing.T, d wrapDeps) {
		script := "out = snapshot(\"tree\") | final\noutput(out | stdout | exit)\n"
		if err := os.WriteFile(filepath.Join(d.dir, "plan.star"), []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		res := runPraytel(t, d.dir, "-e", "plan.star", "--", childBin, "-export-grpc-span", "-span-name", "from-file")
		if res.exit != 0 || !strings.Contains(res.stdout, "from-file") {
			t.Fatalf("exit=%d stdout=%q stderr=%q", res.exit, res.stdout, res.stderr)
		}
	})
}

func TestSettledTraceIsSentOnce(t *testing.T) {
	sweet.Run(t, "a settled output sends each trace with its revision", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "--settle-after=50ms",
			"-e", `tree() | settled | stderr`,
			"--", childBin, "-export-grpc-span", "-span-name", "settles")
		if res.exit != 0 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
		if n := strings.Count(res.stderr, "# trace "); n != 1 || !strings.Contains(res.stderr, "settles") {
			t.Fatalf("want the trace once, got %d:\n%s", n, res.stderr)
		}
	})
}

func TestFilePerTraceFromTheCLI(t *testing.T) {
	sweet.Run(t, "a file path with a placeholder writes one file per trace", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir,
			"-e", `snapshot("tree") | final | file("out/{name}.txt")`,
			"--", childBin, "-export-grpc-span", "-span-name", "per-trace")
		if res.exit != 0 {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
		b, err := os.ReadFile(filepath.Join(d.dir, "out", "per-trace.txt"))
		if err != nil || !strings.Contains(string(b), "per-trace") {
			t.Fatalf("out/per-trace.txt %q %v; stderr %q", b, err, res.stderr)
		}
	})
}

func TestWhereKeepsTheSelection(t *testing.T) {
	sweet.Run(t, "where() keeps the selected spans and drops the rest", wrapFactory, func(t *testing.T, d wrapDeps) {
		keep := runPraytel(t, d.dir,
			"-e", `where(span(name = "child-work")) | snapshot("tree") | final | stdout | exit`,
			"--", childBin, "-export-grpc-span", "-span-name", "child-work")
		if keep.exit != 0 || !strings.Contains(keep.stdout, "child-work") {
			t.Fatalf("exit=%d stdout=%q stderr=%q", keep.exit, keep.stdout, keep.stderr)
		}
		drop := runPraytel(t, d.dir,
			"-e", `where("span{ span:name = 'other' }") | snapshot("tree") | final | stdout`,
			"--", childBin, "-export-grpc-span", "-span-name", "child-work")
		if drop.exit != 0 || strings.Contains(drop.stdout, "child-work") {
			t.Fatalf("exit=%d stdout=%q stderr=%q", drop.exit, drop.stdout, drop.stderr)
		}
	})
}

func TestChecksDecideTheExitCode(t *testing.T) {
	child := []string{"--", childBin, "-export-grpc-span", "-span-name", "child-work"}
	cases := map[string]struct {
		script string
		exit   int
		want   string
	}{
		"some passes": {`some(span(name = "child-work"))`, 0, `praytel: check some(span{ span:name = "child-work" }): PASS`},
		"never fails": {`never(span(name = "child-work"))`, 4, "FAIL"},
		"an anchor that never matched": {`every(span(name = "checkout"), has = span())`, 4,
			`check never exercised: span{ span:name = "checkout" } matched nothing`},
		"a count": {`exactly(1, span(name = "child-work"), name = "one unit of work")`, 0, "check one unit of work: PASS"},
	}
	for name, c := range cases {
		sweet.Run(t, name, wrapFactory, func(t *testing.T, d wrapDeps) {
			res := runPraytel(t, d.dir, append([]string{"--settle-after=50ms", "-e", c.script}, child...)...)
			if res.exit != c.exit || !strings.Contains(res.stderr, c.want) {
				t.Fatalf("exit=%d (want %d) stderr=%q (want %q)", res.exit, c.exit, res.stderr, c.want)
			}
		})
	}

	sweet.Run(t, "ndjson", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, append([]string{"--format=ndjson", "-e", `never(span(name = "child-work"), name = "n")`}, child...)...)
		if res.exit != 4 || !strings.Contains(res.stderr, `{"check":"n","trace_id":`) || !strings.Contains(res.stderr, `"outcome":"fail"`) {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
	})

	sweet.Run(t, "a failed child's code wins", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "-e", `never(span(name = "child-work"))`, "--", childBin, "-export-grpc-span", "-span-name", "child-work", "-exit", "7")
		if res.exit != 7 || !strings.Contains(res.stderr, "FAIL") {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
	})
}

func TestTable(t *testing.T) {
	sweet.Run(t, "a table of the selected spans", wrapFactory, func(t *testing.T, d wrapDeps) {
		res := runPraytel(t, d.dir, "-e", `table(span(name = "child-work"), columns = ["span:name", "span:kind"]) | final | stdout`,
			"--", childBin, "-export-grpc-span", "-span-name", "child-work")
		if res.exit != 0 || !strings.HasPrefix(res.stdout, "span:name\tspan:kind\nchild-work\t") {
			t.Fatalf("exit=%d stdout=%q stderr=%q", res.exit, res.stdout, res.stderr)
		}
	})
}

func TestExecuteErrorNamesThePosition(t *testing.T) {
	sweet.Run(t, "a bad snippet fails before the child runs", wrapFactory, func(t *testing.T, d wrapDeps) {
		marker := filepath.Join(d.dir, "ran")
		res := runPraytel(t, d.dir, "-e", "tree() | stdout", "--", childBin, "-marker", marker)
		if res.exit != 1 || !strings.Contains(res.stderr, "-e[1]:1:1") || !strings.Contains(res.stderr, "needs a trigger") {
			t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("the child ran")
		}
	})
}
