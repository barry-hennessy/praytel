package correlator_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/correlator"
	"github.com/barry-hennessy/praytel/internal/domain"
)

// BenchmarkIngest feeds a correlator spans traces of 50 spans each, every
// span with a log line, the log arriving before its span half the time.
// The cost per span should not grow with how many came before it.
func BenchmarkIngest(b *testing.B) {
	sizes := []int{1_250, 5_000, 20_000}
	if testing.Short() {
		sizes = sizes[:2] // still a step of 4x, to show the cost growing faster
	}
	for _, spans := range sizes {
		b.Run(fmt.Sprintf("spans=%d", spans), func(b *testing.B) {
			for b.Loop() {
				out := make(chan domain.Event, 1024)
				c := correlator.New(observability.LoggingTracer{}, out, correlator.Drain{}, correlator.Limits{})
				done := make(chan struct{})
				go func() {
					for range out {
					}
					close(done)
				}()
				go func() { _ = c.Run(context.Background()) }()
				for i := range spans {
					trace := fmt.Sprintf("%032x", i/50)
					id := fmt.Sprintf("%016x", i)
					parent := ""
					if i%50 > 0 {
						parent = fmt.Sprintf("%016x", i-1-i%3)
					}
					line := domain.LogLine{Body: domain.StringValue("handled"), TraceID: trace, SpanID: id}
					span := domain.Span{TraceID: trace, SpanID: id, ParentSpanID: parent, Name: "op", Ended: true}
					if i%2 == 0 {
						c.Logs() <- line
						c.Spans() <- span
					} else {
						c.Spans() <- span
						c.Logs() <- line
					}
				}
				c.Stop()
				<-done
			}
		})
	}
}
