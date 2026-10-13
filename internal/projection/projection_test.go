package projection_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/praytel/internal/query"
)

func TestReplaySettlementReopenAndFinal(t *testing.T) {
	now := time.Unix(100, 0)
	p := projection.New()
	// Child-before-parent, and the same span ID in another trace.
	p.Observe(domain.SpanUpserted{TraceID: "b", SpanID: "same", Name: "other", EndTime: now}, now)
	p.Observe(domain.SpanClosed{TraceID: "b", SpanID: "same"}, now)
	p.Observe(domain.SpanUpserted{TraceID: "a", SpanID: "child", Name: "bad", Status: domain.SpanStatusError, StartTime: now, EndTime: now.Add(time.Second), Attributes: []domain.Attr{{Key: "n", Value: domain.Int64Value(7)}, {Key: "n", Value: domain.StringValue("duplicate")}}}, now)
	p.Observe(domain.SpanParentLinked{TraceID: "a", SpanID: "child", ParentSpanID: "root"}, now)
	p.Observe(domain.SpanClosed{TraceID: "a", SpanID: "child"}, now)
	p.Observe(domain.SpanUpserted{TraceID: "a", SpanID: "root", Name: "root", EndTime: now}, now)
	p.Observe(domain.SpanClosed{TraceID: "a", SpanID: "root"}, now)
	q, _ := query.Parse(`span{ span:status = error } | matches`)
	r := p.ReadySettled(q, now.Add(time.Second), time.Second)
	if len(r) != 1 || r[0].TraceID != "a" || r[0].Revision != 5 {
		t.Fatalf("first settlement: %+v", r)
	}
	if again := p.ReadySettled(q, now.Add(2*time.Second), time.Second); len(again) != 0 {
		t.Fatalf("duplicate settlement: %+v", again)
	}
	p.Observe(domain.LogReceived{ID: "l", Body: "old body"}, now.Add(2*time.Second))
	p.Observe(domain.LogAssigned{ID: "l", TraceID: "a", SpanID: "child"}, now.Add(2*time.Second))
	r = p.ReadySettled(q, now.Add(3*time.Second), time.Second)
	if len(r) != 1 || r[0].Revision != 6 {
		t.Fatalf("reopened settlement: %+v", r)
	}
	p.Observe(domain.DrainComplete{}, now.Add(4*time.Second))
	final := p.FinalResults(q, now.Add(4*time.Second))
	if len(final) != 1 || final[0].State != projection.Final {
		t.Fatalf("final: %+v", final)
	}
	logs, page, err := p.Logs("a", []string{"l"}, 1)
	if err != nil || page.Matched != 1 || logs[0].Body != "old body" || logs[0].SpanID != "child" {
		t.Fatalf("logs: %+v %+v %v", logs, page, err)
	}
}

func TestTopologyLimitsLossAndTypedValues(t *testing.T) {
	now := time.Unix(200, 0)
	p := projection.New()
	for _, e := range []domain.Event{
		domain.SpanUpserted{TraceID: "t", SpanID: "c", Name: "child", Attributes: []domain.Attr{{Key: "flag", Value: domain.BoolValue(true)}}},
		domain.SpanParentLinked{TraceID: "t", SpanID: "c", ParentSpanID: "r"},
		domain.SpanUpserted{TraceID: "t", SpanID: "r", Name: "root"},
		domain.ForestShed{}, domain.ForestEvicted{TraceID: "t"},
	} {
		p.Observe(e, now)
	}
	top, err := p.TraceTopology("t", 1)
	if err != nil || !top.Page.Truncated || top.Spans[0].ID != "r" || top.Spans[0].Depth != 0 {
		t.Fatalf("topology: %+v %v", top, err)
	}
	if _, err := p.TraceTopology("t", 0); new(projection.InvalidLimitError) == nil || err == nil {
		t.Fatal("expected invalid limit")
	}
	var limitErr *projection.InvalidLimitError
	if _, err := p.TraceTopology("t", 0); !errors.As(err, &limitErr) {
		t.Fatalf("wrong limit error: %v", err)
	}
	if _, err := p.TraceTopology("missing", 1); !errors.Is(err, projection.ErrNotFound) {
		t.Fatalf("wrong not-found: %v", err)
	}
	p.Observe(domain.DrainComplete{}, now)
	q, _ := query.Parse(`span{ span.flag = true }`)
	r := p.FinalResults(q, now)
	want := []projection.IncompleteReason{projection.IncompleteEvicted, projection.IncompleteShed}
	if len(r) != 1 || !reflect.DeepEqual(r[0].IncompleteReasons, want) {
		t.Fatalf("typed/loss result: %+v", r)
	}
	if got := p.Traces(); len(got) != 1 || got[0].TraceID != "t" {
		t.Fatalf("summaries: %+v", got)
	}
}

func TestPendingLogMovesWithoutDuplication(t *testing.T) {
	p := projection.New()
	now := time.Unix(300, 0)
	p.Observe(domain.LogReceived{ID: "x", TraceID: "old", BodyValue: domain.StringValue("typed")}, now)
	p.Observe(domain.LogAssigned{ID: "x", TraceID: "new", SpanID: "s"}, now)
	if got := p.Traces(); len(got) != 2 || got[0].TraceID != "new" || got[0].LogCount != 1 || got[1].LogCount != 0 {
		t.Fatalf("move: %+v", got)
	}
}
