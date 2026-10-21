package correlator

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
)

// TestForestBookkeepingMatchesARecount checks the forest's running log
// count and its index of spans awaiting a parent against working both out
// from scratch, through arrivals in every order, re-parenting, eviction and
// the drain.
func TestForestBookkeepingMatchesARecount(t *testing.T) {
	out := make(chan domain.Event, 1024)
	go func() {
		for range out {
		}
	}()
	c := New(observability.LoggingTracer{}, out, Drain{}, Limits{MaxSpans: 60, MaxLogLines: 80})
	ctx := context.Background()

	check := func(step string) {
		t.Helper()
		lines := 0
		for _, waiting := range c.forest.pending {
			lines += len(waiting)
		}
		for _, sp := range c.forest.spans {
			lines += len(sp.logs)
		}
		if c.forest.logLines != lines {
			t.Fatalf("%s: counted %d log lines, there are %d", step, c.forest.logLines, lines)
		}

		awaiting := 0
		for _, sp := range c.forest.spans {
			if sp.linked || sp.parentSpanID == "" {
				continue
			}
			awaiting++
			if _, ok := c.forest.unlinked[sp.parentSpanID][sp]; !ok {
				t.Fatalf("%s: span %s awaits parent %s but isn't indexed", step, sp.spanID, sp.parentSpanID)
			}
		}
		indexed := 0
		for parent, waiting := range c.forest.unlinked {
			for sp := range waiting {
				indexed++
				if sp.linked || sp.parentSpanID != parent || c.forest.spans[domain.SpanKey{TraceID: sp.traceID, SpanID: sp.spanID}] != sp {
					t.Fatalf("%s: span %s indexed under %s but isn't awaiting it", step, sp.spanID, parent)
				}
			}
		}
		if indexed != awaiting {
			t.Fatalf("%s: %d spans indexed as awaiting a parent, %d are", step, indexed, awaiting)
		}
	}

	rng := rand.New(rand.NewSource(3))
	for i := range 400 {
		trace := fmt.Sprintf("t%d", rng.Intn(12))
		id := fmt.Sprintf("s%d", rng.Intn(150))
		switch rng.Intn(4) {
		case 0, 1:
			parent := ""
			if rng.Intn(4) > 0 {
				parent = fmt.Sprintf("s%d", rng.Intn(150))
			}
			c.handleSpan(ctx, domain.Span{TraceID: trace, SpanID: id, ParentSpanID: parent, Name: "op", Ended: rng.Intn(2) == 0})
		case 2:
			c.handleLog(ctx, domain.LogLine{Body: domain.StringValue("x"), TraceID: trace, SpanID: id})
		case 3:
			c.handleLog(ctx, domain.LogLine{Body: domain.StringValue("x"), TraceID: trace})
		}
		check(fmt.Sprintf("step %d", i))
	}
	c.resolvePending(ctx)
	check("after the drain")
}
