package starlark_test

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/plan/starlark"
)

var update = flag.Bool("update", false, "rewrite the plan goldens in testdata")

// Each testdata/<name>.star compiles to the plan in testdata/<name>.json.
func TestFilesCompileToPlans(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.star"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no scripts: %v", err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".star")
		t.Run(name, func(t *testing.T) {
			p, err := starlark.Compile([]string{filepath.Base(f)}, "testdata", io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			got := p.Explain()
			golden := filepath.Join("testdata", name+".json")
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("plan differs from %s:\n%s", golden, got)
			}
		})
	}
}

func TestSnippets(t *testing.T) {
	cases := map[string]struct {
		src     []string
		outputs int
		err     string // "" means no error
	}{
		"expression":             {[]string{`stream() | each | stdout`}, 1, ""},
		"order of parts is free": {[]string{`stdout | each | stream()`}, 1, ""},
		"list expression":        {[]string{`[tree() | tick | pane("a"), graph() | tick | pane("b")]`}, 2, ""},
		"statements":             {[]string{"o = stream() | each\noutput(o | stdout)"}, 1, ""},
		"two snippets":           {[]string{`stream() | each | stdout`, `stream() | each | stderr`}, 2, ""},
		"none declared":          {[]string{"x = 1"}, 0, "declared no outputs"},
		"two views":              {[]string{`tree() | graph()`}, 0, "one view"},
		"two triggers":           {[]string{`each | final`}, 0, "one trigger"},
		"two sinks":              {[]string{`stdout | stderr`}, 0, "one sink"},
		"missing parts":          {[]string{`stream()`}, 0, "needs a trigger, a sink"},
		"not an output":          {[]string{`42`}, 0, "int, not an output"},
		"invalid plan":           {[]string{`snapshot("tree") | each | stdout`}, 0, "-e[1]:1:1: view snapshot cannot have the trigger each"},
		"invalid in statements":  {[]string{"\n\noutput(tree() | final | pane(\"t\"))"}, 0, "-e[1]:3:7: sink tui needs the trigger tick"},
		"bad section":            {[]string{`snapshot("pie")`}, 0, `unknown section "pie"`},
		"no section":             {[]string{`snapshot()`}, 0, "needs a section"},
		"syntax error":           {[]string{`stream( |`}, 0, "-e[1]:1"},
		"runtime error names it": {[]string{"def f():\n  return 1 | stdout\nf()"}, 0, "-e[1]:2:"},
		"unknown name":           {[]string{`teapot()`}, 0, "undefined: teapot"},
		"endless loop stops":     {[]string{"while True:\n  pass"}, 0, "too many steps"},
		"missing file":           {[]string{`nope.star`}, 0, "nope.star"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p, err := starlark.Compile(c.src, t.TempDir(), io.Discard)
			if c.err == "" {
				if err != nil {
					t.Fatal(err)
				}
				if len(p.Outputs) != c.outputs {
					t.Fatalf("%d outputs, want %d", len(p.Outputs), c.outputs)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Fatalf("error %v, want it to contain %q", err, c.err)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("lib/ok.star", "def s():\n  return stream() | each | stdout\n")
	write("lib/declares.star", "output(stream() | each | stdout)\nx = 1\n")
	write("lib/a.star", `load("lib/b.star", "x")`+"\n")
	write("lib/b.star", `load("lib/a.star", "y")`+"\n")

	cases := map[string]string{
		`load("lib/ok.star", "s")` + "\noutput(s())": "",
		`load("lib/declares.star", "x")`:             "cannot declare outputs while it loads",
		`load("lib/a.star", "x")`:                    "cycle",
		`load("../escape.star", "x")`:                "inside the current directory",
		`load("/etc/passwd.star", "x")`:              "inside the current directory",
		`load("lib/ok.py", "x")`:                     "inside the current directory",
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			_, err := starlark.Compile([]string{src}, dir, io.Discard)
			switch {
			case want == "" && err != nil:
				t.Fatal(err)
			case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
				t.Fatalf("error %v, want it to contain %q", err, want)
			}
		})
	}
}

func TestPrintGoesToTheWriter(t *testing.T) {
	var out bytes.Buffer
	if _, err := starlark.Compile([]string{"print(\"hi\")\noutput(stream() | each | stdout)"}, t.TempDir(), &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "hi\n" {
		t.Fatalf("print wrote %q", out.String())
	}
}

func FuzzSnippet(f *testing.F) {
	for _, s := range []string{
		`stream() | each | stdout`,
		`snapshot("tree", "graph", logs = True) | final | stdout | exit`,
		"o = tree() | tick\noutput(o | pane(\"x\"))",
		`attrs("a -b") | no_attrs() | file("x")`,
		`[1, stdout][1] | each`,
	} {
		f.Add(s)
	}
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, src string) {
		if strings.Contains(src, "load") {
			t.Skip("load reads files")
		}
		p, err := starlark.Compile([]string{src}, dir, io.Discard)
		if err == nil {
			if verr := p.Validate(); verr != nil {
				t.Fatalf("Compile returned an invalid plan for %q: %v", src, verr)
			}
		}
	})
}

// The plans from flags and from their -e forms are the same; see
// cmd/praytel/internal/args for the flag side. This checks the -e side is
// a valid plan of the expected shape.
func TestFlagForms(t *testing.T) {
	p, err := starlark.Compile([]string{`no_attrs() | snapshot("tree") | final | stdout | exit`}, t.TempDir(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	o := p.Outputs[0]
	if o.Name != "snapshot" || !o.Sink.Exit || o.Emit != plan.Final {
		t.Fatalf("%+v", o)
	}
}
