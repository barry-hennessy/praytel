package args_test

import (
	"io"
	"testing"

	"github.com/barry-hennessy/praytel/cmd/praytel/internal/args"
)

func TestParseTUI(t *testing.T) {
	cfg, err := args.Parse([]string{"--tui", "--", "true"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.TUI {
		t.Fatalf("TUI was not enabled: %+v", cfg)
	}
}

func TestParseSnapshotViews(t *testing.T) {
	cases := []struct {
		flags       []string
		tree, graph bool
		ok          bool
	}{
		{[]string{"--snapshot=tree"}, true, false, true},
		{[]string{"--snapshot=graph"}, false, true, true},
		{[]string{"--snapshot=tree graph"}, true, true, true},
		{[]string{"--snapshot=graph tree"}, true, true, true},
		{[]string{"--snapshot=tree tree"}, true, false, true},
		{[]string{"--snapshot="}, false, false, false},
		{[]string{"--snapshot=logs"}, false, false, false},
		{[]string{"--snapshot"}, false, false, false},
		{[]string{"--snapshot=tree", "--tui"}, false, false, false},
		{[]string{"--snapshot-attrs=a"}, false, false, false},
		{[]string{"--snapshot-logs"}, false, false, false},
	}
	for _, c := range cases {
		cfg, err := args.Parse(append(c.flags, "--", "true"), io.Discard)
		if (err == nil) != c.ok {
			t.Errorf("%v: err = %v, want ok=%v", c.flags, err, c.ok)
			continue
		}
		if c.ok && (!cfg.Snapshot || cfg.SnapshotTree != c.tree || cfg.SnapshotGraph != c.graph) {
			t.Errorf("%v: %+v", c.flags, cfg)
		}
	}
}

func TestParseSnapshotAttrs(t *testing.T) {
	cases := []struct {
		flag string
		key  string
		want bool
	}{
		{"", "a", false},
		{"--snapshot-attrs", "a", true},
		{"--snapshot-attrs=a b", "a", true},
		{"--snapshot-attrs=a b", "c", false},
		{"--snapshot-attrs=a b -a", "a", false},
		{"--snapshot-attrs=-c", "a", true},
		{"--snapshot-attrs=-c", "c", false},
	}
	for _, c := range cases {
		argv := []string{"--snapshot=tree"}
		if c.flag != "" {
			argv = append(argv, c.flag)
		}
		cfg, err := args.Parse(append(argv, "--", "true"), io.Discard)
		if err != nil {
			t.Fatalf("%q: %v", c.flag, err)
		}
		if got := cfg.SnapshotAttrs.Match(c.key); got != c.want {
			t.Errorf("%q: Match(%q) = %v, want %v", c.flag, c.key, got, c.want)
		}
	}
}
