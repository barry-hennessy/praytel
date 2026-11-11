package pipeline_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/attrspec"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/pipeline"
	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/render/tui"
)

func lt() observability.LoggingTracer {
	return observability.LoggingTracer{Log: *slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func attr(k, v string) domain.Attr { return domain.Attr{Key: k, Value: domain.StringValue(v)} }

// run is a small instrumented child: a request span with a query below it,
// one log on each, and a completed drain.
func run() []domain.Event {
	return []domain.Event{
		domain.SpanUpserted{TraceID: "t", SpanID: "r", Name: "GET /", Component: "api",
			Attributes: []domain.Attr{attr("http.method", "GET"), attr("http.port", "80")}},
		domain.LogReceived{ID: "1", Source: domain.SourceStdout, TraceID: "t", SpanID: "r", SpanName: "GET /", Body: "start"},
		domain.SpanUpserted{TraceID: "t", SpanID: "q", Name: "query", Component: "db"},
		domain.SpanParentLinked{TraceID: "t", SpanID: "q", ParentSpanID: "r"},
		domain.LogReceived{ID: "2", Source: domain.SourceStderr, TraceID: "t", SpanID: "q", SpanName: "query", Body: "rows=1"},
		domain.SpanClosed{TraceID: "t", SpanID: "q"},
		domain.SpanClosed{TraceID: "t", SpanID: "r"},
		domain.DrainComplete{},
	}
}

func feed(evs []domain.Event) <-chan domain.Event {
	ch := make(chan domain.Event, len(evs))
	for _, ev := range evs {
		ch <- ev
	}
	close(ch)
	return ch
}

func snapshotOutput(sink plan.Sink) plan.Output {
	return plan.Output{
		Name:   "snapshot",
		Stages: []plan.Stage{{Kind: plan.StageAttrs, Globs: "http.* -http.port"}},
		View:   plan.View{Kind: plan.ViewSnapshot, Sections: []string{plan.SectionTree, plan.SectionGraph}},
		Emit:   plan.Final,
		Sink:   sink,
	}
}

func streamOutput() plan.Output {
	return plan.Output{View: plan.View{Kind: plan.ViewStream}, Emit: plan.Each, Sink: plan.Sink{Kind: plan.SinkStdout}}
}

type result struct {
	code           int
	stdout, stderr string
}

func drive(t *testing.T, p plan.Plan, evs []domain.Event, childCode int, ctx context.Context) result {
	t.Helper()
	var out, errOut bytes.Buffer
	d, err := pipeline.Compile(p, pipeline.Deps{LT: lt(), Events: feed(evs), Stdout: &out, Stderr: &errOut})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Run(ctx); err != nil {
		t.Fatal(err)
	}
	code := d.Close(childCode)
	return result{code: code, stdout: out.String(), stderr: errOut.String()}
}

// The same events through the driver and through tui.Snapshot give the
// same text: the snapshot view reads the shared forest through the stage.
func TestSnapshotMatchesDirect(t *testing.T) {
	r := drive(t, plan.Plan{Outputs: []plan.Output{snapshotOutput(plan.Sink{Kind: plan.SinkStdout, Exit: true})}},
		run(), 0, context.Background())

	opts := tui.SnapshotOptions{Tree: true, Graph: true}
	opts.Attrs, _ = attrspec.Parse("http.* -http.port")
	want := tui.Snapshot(feed(run()), opts)

	if r.code != 0 || r.stdout != want.Text {
		t.Fatalf("code %d\n got:\n%s\nwant:\n%s\nstderr: %s", r.code, r.stdout, want.Text, r.stderr)
	}
	if strings.Contains(r.stdout, "http.port") || !strings.Contains(r.stdout, "http.method") {
		t.Fatalf("the attrs stage did not apply:\n%s", r.stdout)
	}
}

// Two outputs in one run: a snapshot to a file and the stream to stdout.
func TestFanOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tree.txt")
	p := plan.Plan{Outputs: []plan.Output{
		snapshotOutput(plan.Sink{Kind: plan.SinkFile, Path: path}),
		streamOutput(),
	}}
	r := drive(t, p, run(), 0, context.Background())

	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(file), "# praytel-snapshot v1\n## tree\n") || !strings.Contains(string(file), "db.query") {
		t.Fatalf("file:\n%s", file)
	}
	wantStream := "stdout GET / t r start\nstderr query t q rows=1\n"
	if r.stdout != wantStream {
		t.Fatalf("stream %q, want %q", r.stdout, wantStream)
	}
}

func TestExitSink(t *testing.T) {
	p := plan.Plan{Outputs: []plan.Output{snapshotOutput(plan.Sink{Kind: plan.SinkStdout, Exit: true})}}

	t.Run("a failed child's code wins and its output is replayed", func(t *testing.T) {
		r := drive(t, p, run(), 7, context.Background())
		if r.code != 7 || r.stdout != "" || r.stderr != "start\nrows=1\n" {
			t.Fatalf("%+v", r)
		}
	})

	t.Run("an incomplete output writes nothing and exits 3", func(t *testing.T) {
		evs := run()
		r := drive(t, p, evs[:len(evs)-1], 0, context.Background())
		if r.code != plan.IncompleteExit || r.stdout != "" ||
			!strings.Contains(r.stderr, "praytel: snapshot incomplete: the drain never completed") {
			t.Fatalf("%+v", r)
		}
	})

	t.Run("a run cut short reports it", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := drive(t, p, run(), 0, ctx)
		if r.code != plan.IncompleteExit || !strings.Contains(r.stderr, "interrupted before the run finished") {
			t.Fatalf("%+v", r)
		}
	})

	t.Run("an output without an exit sink does not change the code", func(t *testing.T) {
		r := drive(t, plan.Plan{Outputs: []plan.Output{streamOutput()}}, nil, 0, context.Background())
		if r.code != 0 {
			t.Fatalf("%+v", r)
		}
	})
}

func TestCompileRejects(t *testing.T) {
	cases := map[string]plan.Plan{
		"invalid plan": {},
		"tick on a sink that is not a pane": {Outputs: []plan.Output{{View: plan.View{Kind: plan.ViewStream}, Emit: plan.Tick,
			Sink: plan.Sink{Kind: plan.SinkStdout}}}},
		"exit on a view that cannot report": {Outputs: []plan.Output{{View: plan.View{Kind: plan.ViewStream},
			Emit: plan.Each, Sink: plan.Sink{Kind: plan.SinkStdout, Exit: true}}}},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := pipeline.Compile(p, pipeline.Deps{LT: lt(), Stdout: io.Discard, Stderr: io.Discard}); err == nil {
				t.Fatal("compiled")
			}
		})
	}
}
