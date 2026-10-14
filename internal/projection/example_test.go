package projection_test

import (
	"fmt"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/praytel/internal/query"
)

func ExampleProjection_ReadySettled() {
	p := projection.New()
	now := time.Unix(1, 0)
	p.Observe(domain.SpanUpserted{TraceID: "trace", SpanID: "span", Name: "request", Status: domain.SpanStatusError}, now)
	p.Observe(domain.SpanClosed{TraceID: "trace", SpanID: "span"}, now)
	q, _ := query.Parse(`span{ span:status = error } | matches`)
	results := p.ReadySettled(q, now.Add(time.Second), time.Second)
	fmt.Println(results[0].TraceID, results[0].Evidence.SpanIDs)
	// Output: trace [span]
}

func ExampleProjection_TraceTopology() {
	p := projection.New()
	p.Observe(domain.SpanUpserted{TraceID: "trace", SpanID: "root", Name: "request"}, time.Time{})
	p.Observe(domain.SpanUpserted{TraceID: "trace", SpanID: "child", Name: "database"}, time.Time{})
	p.Observe(domain.SpanParentLinked{TraceID: "trace", SpanID: "child", ParentSpanID: "root"}, time.Time{})
	topology, _ := p.TraceTopology("trace", 10)
	for _, span := range topology.Spans {
		fmt.Println(span.Depth, span.Name)
	}
	// Output:
	// 0 request
	// 1 database
}
