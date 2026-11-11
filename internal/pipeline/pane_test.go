package pipeline_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/pipeline"
	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/render/tui"
)

func panePlan(extra ...plan.Output) plan.Plan {
	p := plan.Plan{Outputs: []plan.Output{
		{Stages: []plan.Stage{{Kind: plan.StageNoAttrs}}, View: plan.View{Kind: plan.ViewTree}, Emit: plan.Tick, Sink: plan.Sink{Kind: plan.SinkTUI, Pane: "calls"}},
		{View: plan.View{Kind: plan.ViewStream}, Emit: plan.Tick, Sink: plan.Sink{Kind: plan.SinkTUI}},
	}}
	p.Outputs = append(p.Outputs, extra...)
	return p
}

func TestTUIPanes(t *testing.T) {
	t.Run("compile into panes in plan order", func(t *testing.T) {
		d, err := pipeline.Compile(panePlan(), pipeline.Deps{LT: lt(), Events: feed(nil), Stdout: io.Discard, Stderr: io.Discard})
		if err != nil {
			t.Fatal(err)
		}
		ps := d.Panes()
		if len(ps) != 2 || ps[0].Name != "calls" || ps[0].Kind != tui.PaneTree || len(ps[0].Stages) != 1 ||
			ps[1].Name != "stream" || ps[1].Kind != tui.PaneStream {
			t.Fatalf("panes %+v", ps)
		}
	})

	t.Run("other outputs must not write to the terminal", func(t *testing.T) {
		_, err := pipeline.Compile(panePlan(streamOutput()), pipeline.Deps{LT: lt(), Stdout: io.Discard, Stderr: io.Discard})
		if err == nil || !strings.Contains(err.Error(), "owns the terminal") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("the TUI gets every event; a file output runs beside it", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "snap.txt")
		p := panePlan(snapshotOutput(plan.Sink{Kind: plan.SinkFile, Path: path}))
		d, err := pipeline.Compile(p, pipeline.Deps{LT: lt(), Events: feed(run()), Stdout: io.Discard, Stderr: io.Discard})
		if err != nil {
			t.Fatal(err)
		}
		got := make(chan int)
		go func() {
			n := 0
			for range d.TUIEvents() {
				n++
			}
			got <- n
		}()
		if err := d.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if n := <-got; n != len(run()) {
			t.Fatalf("TUI got %d events, want %d", n, len(run()))
		}
		d.Close(0)
		if b, err := os.ReadFile(path); err != nil || !strings.Contains(string(b), "db.query") {
			t.Fatalf("file %q, %v", b, err)
		}
	})

	t.Run("a TUI that ends does not block the driver", func(t *testing.T) {
		d, err := pipeline.Compile(panePlan(), pipeline.Deps{LT: lt(), Events: feed(run()), Stdout: io.Discard, Stderr: io.Discard})
		if err != nil {
			t.Fatal(err)
		}
		d.TUIEnded()
		d.TUIEnded() // twice is fine
		if err := d.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}
