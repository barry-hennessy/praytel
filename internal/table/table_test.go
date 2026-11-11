package table_test

import (
	"strings"
	"testing"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/praytel/internal/table"
	"github.com/barry-hennessy/praytel/internal/view"
)

func forest() *projection.Forest {
	f := projection.New()
	at := func(ms int) time.Time { return time.Unix(0, 0).Add(time.Duration(ms) * time.Millisecond) }
	for _, ev := range []domain.Event{
		domain.SpanUpserted{TraceID: "b", SpanID: "t2", Name: "test", StartTime: at(0), EndTime: at(1500),
			Attributes: []domain.Attr{{Key: "test.name", Value: domain.StringValue("TestB")}, {Key: "input", Value: domain.Int64Value(3)}}},
		domain.SpanUpserted{TraceID: "a", SpanID: "t1", Name: "test", StartTime: at(0), EndTime: at(20),
			Attributes: []domain.Attr{{Key: "test.name", Value: domain.StringValue("TestA\tx")}, {Key: "input", Value: domain.Int64Value(2)}}},
		domain.SpanUpserted{TraceID: "a", SpanID: "other", Name: "other"},
	} {
		f.Apply(ev)
	}
	return f
}

func TestTSV(t *testing.T) {
	v, err := table.New(`span{ span:name = "test" }`, []string{"test.name", "input", "span:duration", "missing"}, false)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(v.Render(forest(), view.Viewport{}), "\n")
	want := "test.name\tinput\tspan:duration\tmissing\n" +
		"TestA x\t2\t20ms\t\n" +
		"TestB\t3\t1.5s\t"
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
}

func TestNDJSON(t *testing.T) {
	v, err := table.New(`span{ span:name = "test" }`, []string{"test.name", "input"}, true)
	if err != nil {
		t.Fatal(err)
	}
	got := v.Render(forest(), view.Viewport{})
	if len(got) != 2 || got[0] != `{"input":"2","test.name":"TestA\tx"}` {
		t.Fatalf("got %q", got)
	}
}

func TestBadColumn(t *testing.T) {
	if _, err := table.New(`span{}`, []string{"span:nope"}, false); err == nil {
		t.Fatal("bad column accepted")
	}
}
