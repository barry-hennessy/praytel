// Package starlark is the --execute front-end: it runs Starlark snippets and
// files once and turns what they declare into a plan. Scripts see only the
// builtins here. They cannot read files, the time or random values, so a
// script always gives the same plan. Starlark never sees events. See
// design/PIPELINE.md, "The --execute flag".
package starlark

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	sl "go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/barry-hennessy/praytel/internal/plan"
)

// fileOptions lets scripts use top-level loops and conditionals (to make
// many outputs or checks) and reassign globals.
var fileOptions = &syntax.FileOptions{
	Set:             true,
	While:           true,
	TopLevelControl: true,
	GlobalReassign:  true,
}

// Compile runs each value of --execute in order and returns the plan they
// declare. A value that ends with ".star" is a path to a file, relative to
// dir; any other value is Starlark code. print() writes to stdout's
// neighbour, w.
func Compile(values []string, dir string, w io.Writer) (plan.Plan, error) {
	c := &compiler{dir: dir, print: w, modules: map[string]*module{}}
	for i, v := range values {
		var err error
		if strings.HasSuffix(v, ".star") {
			err = c.file(v)
		} else {
			err = c.snippet(fmt.Sprintf("-e[%d]", i+1), v)
		}
		if err != nil {
			return plan.Plan{}, err
		}
	}
	return c.finish()
}

type located struct {
	out plan.Output
	pos syntax.Position
	// taken is a count that every() took over: not an output of its own.
	taken bool
}

type compiler struct {
	dir     string
	print   io.Writer
	outputs []*located
	modules map[string]*module
}

type module struct {
	globals sl.StringDict
	err     error
	loading bool
}

const collectorKey = "praytel.collector"

// maxSteps bounds the work of one script or module.
const maxSteps = 10_000_000

func (c *compiler) thread(name string, collect bool) *sl.Thread {
	th := &sl.Thread{
		Name: name,
		Load: c.load,
		Print: func(_ *sl.Thread, msg string) {
			_, _ = fmt.Fprintln(c.print, msg)
		},
	}
	if collect {
		th.SetLocal(collectorKey, c)
	}
	// A plan is small: a script that runs this long is stuck in a loop.
	th.SetMaxExecutionSteps(maxSteps)
	return th
}

func (c *compiler) snippet(name, src string) error {
	if _, err := fileOptions.ParseExpr(name, src, 0); err == nil {
		v, err := sl.EvalOptions(fileOptions, c.thread(name, true), name, src, predeclared)
		if err != nil {
			return scriptError(err)
		}
		return c.addValue(v, syntax.MakePosition(&name, 1, 1))
	}
	_, err := sl.ExecFileOptions(fileOptions, c.thread(name, true), name, src, predeclared)
	return scriptError(err)
}

func (c *compiler) file(p string) error {
	src, err := os.ReadFile(filepath.Join(c.dir, filepath.FromSlash(p)))
	if err != nil {
		return fmt.Errorf("--execute %s: %w", p, err)
	}
	_, err = sl.ExecFileOptions(fileOptions, c.thread(p, true), p, src, predeclared)
	return scriptError(err)
}

// load runs a module for load(). Its path is relative to dir and may not
// leave it. A module cannot declare outputs while it loads; it can define
// the functions that do.
func (c *compiler) load(_ *sl.Thread, name string) (sl.StringDict, error) {
	clean := path.Clean(name)
	if path.IsAbs(name) || clean == ".." || strings.HasPrefix(clean, "../") || !strings.HasSuffix(clean, ".star") {
		return nil, fmt.Errorf("load %q: a module is a .star path inside the current directory", name)
	}
	if m, ok := c.modules[clean]; ok {
		if m.loading {
			return nil, fmt.Errorf("load %q: cycle", name)
		}
		return m.globals, m.err
	}
	m := &module{loading: true}
	c.modules[clean] = m
	src, err := os.ReadFile(filepath.Join(c.dir, filepath.FromSlash(clean)))
	if err == nil {
		m.globals, err = sl.ExecFileOptions(fileOptions, c.thread(clean, false), clean, src, predeclared)
	}
	m.err, m.loading = err, false
	return m.globals, m.err
}

func (c *compiler) addValue(v sl.Value, pos syntax.Position) error {
	switch v := v.(type) {
	case sl.NoneType, *count:
		// A count declared itself when it was made.
		return nil
	case *part:
		o, err := v.output()
		if err != nil {
			return fmt.Errorf("%s: %w", pos, err)
		}
		c.outputs = append(c.outputs, &located{out: o, pos: pos})
		return nil
	case *sl.List:
		for e := range v.Elements() {
			if err := c.addValue(e, pos); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%s: the value is a %s, not an output", pos, v.Type())
	}
}

func (c *compiler) finish() (plan.Plan, error) {
	var p plan.Plan
	var errs []error
	for _, l := range c.outputs {
		if l.taken {
			continue
		}
		if err := (plan.Plan{Outputs: []plan.Output{l.out}}).Validate(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %s", l.pos, strings.TrimPrefix(err.Error(), "output 1: ")))
			continue
		}
		p.Outputs = append(p.Outputs, l.out)
	}
	if len(errs) > 0 {
		return plan.Plan{}, errors.Join(errs...)
	}
	if len(p.Outputs) == 0 {
		return plan.Plan{}, errors.New("--execute declared no outputs")
	}
	return p, nil
}

// scriptError keeps a Starlark backtrace, which names the file, line and
// column of each call.
func scriptError(err error) error {
	if ee, ok := errors.AsType[*sl.EvalError](err); ok {
		return errors.New(ee.Backtrace())
	}
	return err
}
