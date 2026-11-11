package pipeline_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/pipeline"
	"github.com/barry-hennessy/praytel/internal/plan"
)

// clock is a fake clock the test moves and the driver reads.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) set(t time.Time) { c.mu.Lock(); c.t = t; c.mu.Unlock() }

// syncBuffer is a buffer the driver writes and the test reads.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuffer) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// rig runs a driver on unbuffered channels, so each send returns once the
// driver has taken the event or tick; a second tick shows the first one was
// handled.
type rig struct {
	events chan domain.Event
	tick   chan time.Time
	clock  *clock
	out    *syncBuffer
	done   chan error
	d      *pipeline.Driver
}

const settleAfter = 500 * time.Millisecond

var t0 = time.Unix(1000, 0)

func newRig(t *testing.T, p plan.Plan) *rig {
	t.Helper()
	r := &rig{
		events: make(chan domain.Event),
		tick:   make(chan time.Time),
		clock:  &clock{t: t0},
		out:    &syncBuffer{},
		done:   make(chan error, 1),
	}
	d, err := pipeline.Compile(p, pipeline.Deps{
		LT: lt(), Events: r.events, Stdout: r.out, Stderr: io.Discard,
		SettleAfter: settleAfter, Now: r.clock.now, Tick: r.tick,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.d = d
	go func() { r.done <- d.Run(context.Background()) }()
	return r
}

func (r *rig) send(evs ...domain.Event) {
	for _, ev := range evs {
		r.events <- ev
	}
}

// at moves the clock and lets the driver settle what is quiet by then. The
// first tick, at the old time, makes sure the driver has applied every event
// sent so far before the clock moves.
func (r *rig) at(t time.Time) {
	r.sync()
	r.clock.set(t)
	r.tick <- t
	r.tick <- t
}

// sync returns once the driver has handled everything sent before it.
func (r *rig) sync() {
	now := r.clock.now()
	r.tick <- now
	r.tick <- now
}

func (r *rig) end(t *testing.T) {
	t.Helper()
	close(r.events)
	if err := <-r.done; err != nil {
		t.Fatal(err)
	}
	r.d.Close(0)
}

var header = regexp.MustCompile(`(?m)^# trace (\S+) revision (\d+) (\S+)$`)

func headers(s string) []string {
	var out []string
	for _, m := range header.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1]+" "+m[3])
	}
	return out
}

func treeSettled() plan.Plan {
	return plan.Plan{Outputs: []plan.Output{{
		View: plan.View{Kind: plan.ViewTree}, Emit: plan.Settled, Sink: plan.Sink{Kind: plan.SinkStdout},
	}}}
}

func TestSettledOutput(t *testing.T) {
	t.Run("settled, live again, settled again; a live trace goes out at drain", func(t *testing.T) {
		r := newRig(t, treeSettled())
		r.send(
			domain.SpanUpserted{TraceID: "a", SpanID: "r", Name: "first"},
			domain.SpanClosed{TraceID: "a", SpanID: "r"},
		)
		r.at(t0.Add(settleAfter - time.Millisecond))
		if got := headers(r.out.String()); len(got) != 0 {
			t.Fatalf("sent before it settled: %v", got)
		}
		r.at(t0.Add(settleAfter))
		if got := headers(r.out.String()); strings.Join(got, ",") != "a settled" {
			t.Fatalf("after settling: %v", got)
		}

		r.sync()
		r.clock.set(t0.Add(2 * settleAfter))
		r.send(
			domain.SpanUpserted{TraceID: "a", SpanID: "late", Name: "second"},
			domain.SpanClosed{TraceID: "a", SpanID: "late"},
			domain.SpanUpserted{TraceID: "b", SpanID: "r", Name: "busy"},
		)
		r.at(t0.Add(3 * settleAfter))
		r.end(t)

		out := r.out.String()
		if got := strings.Join(headers(out), ","); got != "a settled,a settled,b final" {
			t.Fatalf("headers %s\n%s", got, out)
		}
		revs := header.FindAllStringSubmatch(out, -1)
		if revs[0][2] == revs[1][2] {
			t.Fatalf("the second send of a has the same revision %s", revs[0][2])
		}
		if !strings.Contains(out, "second") {
			t.Fatalf("the late span is missing:\n%s", out)
		}
	})

	t.Run("an evicted trace is never sent", func(t *testing.T) {
		r := newRig(t, treeSettled())
		r.send(domain.SpanUpserted{TraceID: "a", SpanID: "r"}, domain.ForestEvicted{TraceID: "a"})
		r.at(t0.Add(time.Hour))
		r.end(t)
		if out := r.out.String(); out != "" {
			t.Fatalf("sent %q", out)
		}
	})
}

func TestFilePerTrace(t *testing.T) {
	dir := t.TempDir()
	pattern := filepath.Join(dir, "golden", "{test.name}.txt")
	p := plan.Plan{Outputs: []plan.Output{{
		View: plan.View{Kind: plan.ViewSnapshot, Sections: []string{plan.SectionTree}},
		Emit: plan.Final,
		Sink: plan.Sink{Kind: plan.SinkFile, Path: pattern},
	}}}
	test := func(trace, name string) []domain.Event {
		return []domain.Event{
			domain.SpanUpserted{TraceID: trace, SpanID: "root", Name: "test." + name,
				Attributes: []domain.Attr{attr("test.name", name)}},
			domain.SpanUpserted{TraceID: trace, SpanID: "work", Name: "work-" + trace},
			domain.SpanParentLinked{TraceID: trace, SpanID: "work", ParentSpanID: "root"},
			domain.SpanClosed{TraceID: trace, SpanID: "work"},
			domain.SpanClosed{TraceID: trace, SpanID: "root"},
		}
	}
	evs := append(test("t1", "TestA"), test("t2", "../escape")...)
	evs = append(evs, domain.SpanUpserted{TraceID: "t3", SpanID: "x", Name: "no-test-name"})
	evs = append(evs, domain.DrainComplete{})

	var errs bytes.Buffer
	d, err := pipeline.Compile(p, pipeline.Deps{LT: lt(), Events: feed(evs), Stdout: io.Discard, Stderr: &errs})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.Close(0)

	a, err := os.ReadFile(filepath.Join(dir, "golden", "TestA.txt"))
	if err != nil || !strings.Contains(string(a), "work-t1") || strings.Contains(string(a), "work-t2") {
		t.Fatalf("TestA.txt %q %v", a, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "golden", "_.._escape.txt")); err != nil {
		t.Fatalf("the unsafe name was not made safe: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); err == nil {
		t.Fatal("a span attribute wrote outside the directory")
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "golden"))
	if len(entries) != 2 {
		t.Fatalf("want 2 files (no file for the trace without test.name), got %d", len(entries))
	}
}
