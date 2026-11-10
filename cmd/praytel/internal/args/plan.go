package args

import (
	"os"

	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/plan/starlark"
)

// Plan is the plan the flags make: the Starlark of --execute, or the mode
// flags, which are shorthand for plans. See design/PIPELINE.md.
func (c Config) Plan() (plan.Plan, error) {
	if len(c.Execute) > 0 {
		return starlark.Compile(c.Execute, ".", os.Stderr)
	}
	p := c.flagPlan()
	return p, p.Validate()
}

func (c Config) flagPlan() plan.Plan {
	switch {
	case c.Snapshot:
		return plan.Plan{Outputs: []plan.Output{c.snapshotOutput()}}
	case c.TUI:
		pane := func(view string) plan.Output {
			return plan.Output{
				Name: view,
				View: plan.View{Kind: view},
				Emit: plan.Tick,
				Sink: plan.Sink{Kind: plan.SinkTUI, Pane: view},
			}
		}
		return plan.Plan{Outputs: []plan.Output{pane(plan.ViewTree), pane(plan.ViewGraph)}}
	default:
		return plan.Plan{Outputs: []plan.Output{{
			Name: "stream",
			View: plan.View{Kind: plan.ViewStream},
			Emit: plan.Each,
			Sink: plan.Sink{Kind: plan.SinkStdout},
		}}}
	}
}

func (c Config) snapshotOutput() plan.Output {
	st := plan.Stage{Kind: plan.StageNoAttrs}
	if c.snapshotGlobs != nil {
		st = plan.Stage{Kind: plan.StageAttrs, Globs: *c.snapshotGlobs}
	}
	var sections []string
	if c.SnapshotTree {
		sections = append(sections, plan.SectionTree)
	}
	if c.SnapshotGraph {
		sections = append(sections, plan.SectionGraph)
	}
	return plan.Output{
		Name:   "snapshot",
		Stages: []plan.Stage{st},
		View:   plan.View{Kind: plan.ViewSnapshot, Sections: sections, Logs: c.SnapshotLogs},
		Emit:   plan.Final,
		Sink:   plan.Sink{Kind: plan.SinkStdout, Exit: true},
	}
}
