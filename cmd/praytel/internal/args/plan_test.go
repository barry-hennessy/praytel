package args_test

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/cmd/praytel/internal/args"
)

var updatePlans = flag.Bool("update", false, "rewrite the plan goldens in testdata/plans")

// Each flag set compiles to the plan in testdata/plans/<name>.json.
func TestFlagsCompileToPlans(t *testing.T) {
	cases := map[string][]string{
		"stream":                {},
		"tui":                   {"--tui"},
		"snapshot-tree":         {"--snapshot=tree"},
		"snapshot-graph-tree":   {"--snapshot=graph tree"},
		"snapshot-attrs-all":    {"--snapshot=tree graph", "--snapshot-attrs"},
		"snapshot-attrs-select": {"--snapshot=tree", "--snapshot-attrs=http.* -http.port"},
		"snapshot-logs":         {"--snapshot=tree", "--snapshot-logs"},
	}
	for name, flags := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := args.Parse(append(flags, "--", "true"), io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			pl, err := cfg.Plan()
			if err != nil {
				t.Fatalf("invalid plan: %v", err)
			}
			got := pl.Explain()

			path := filepath.Join("testdata", "plans", name+".json")
			if *updatePlans {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("plan differs from %s:\n%s", path, got)
			}
		})
	}
}

func TestPrintPlanNeedsNoCommand(t *testing.T) {
	cfg, err := args.Parse([]string{"--print-plan", "--tui"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PrintPlan || !cfg.TUI {
		t.Fatalf("cfg = %+v", cfg)
	}
	if _, err := args.Parse([]string{"--tui"}, io.Discard); err == nil {
		t.Fatal("a run without --print-plan still needs a command")
	}
}

// Each mode flag set and its documented --execute form make the same plan.
func TestFlagsEqualTheirExecuteForms(t *testing.T) {
	cases := []struct {
		flags   []string
		execute []string
	}{
		{nil, []string{`stream() | each | stdout`}},
		{[]string{"--tui"}, []string{`tree() | tick | pane("tree")`, `graph() | tick | pane("graph")`}},
		{[]string{"--snapshot=tree"}, []string{`no_attrs() | snapshot("tree") | final | stdout | exit`}},
		{[]string{"--snapshot=tree graph", "--snapshot-attrs"}, []string{`attrs() | snapshot("tree", "graph") | final | stdout | exit`}},
		{[]string{"--snapshot=graph", "--snapshot-attrs=a.* -a.b", "--snapshot-logs"},
			[]string{`attrs("a.* -a.b") | snapshot("graph", logs = True) | final | stdout | exit`}},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.flags, " "), func(t *testing.T) {
			byFlags, err := args.Parse(append(c.flags, "--", "true"), io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			var argv []string
			for _, e := range c.execute {
				argv = append(argv, "-e", e)
			}
			byExecute, err := args.Parse(append(argv, "--", "true"), io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			a, err := byFlags.Plan()
			if err != nil {
				t.Fatal(err)
			}
			b, err := byExecute.Plan()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a.Explain(), b.Explain()) {
				t.Fatalf("flags:\n%s\nexecute:\n%s", a.Explain(), b.Explain())
			}
		})
	}
}

func TestExecuteExcludesModeFlags(t *testing.T) {
	for _, mode := range []string{"--tui", "--snapshot=tree"} {
		if _, err := args.Parse([]string{mode, "-e", "x", "--", "true"}, io.Discard); err == nil {
			t.Fatalf("%s with -e parsed", mode)
		}
	}
}
