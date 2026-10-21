package filter_test

import (
	"fmt"
	"testing"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/filter"
)

// BenchmarkObserveWithFocus feeds a focused filter traces of 50 spans, each
// with a log, half the traces off the focused path so their logs are held.
// The cost per event should not grow with how many came before it.
func BenchmarkObserveWithFocus(b *testing.B) {
	sizes := []int{500, 2_000, 8_000}
	if testing.Short() {
		sizes = sizes[:2] // still a step of 4x, to show the cost growing faster
	}
	for _, spans := range sizes {
		b.Run(fmt.Sprintf("spans=%d", spans), func(b *testing.B) {
			for b.Loop() {
				f, err := filter.New(filter.Query{Focus: []string{"orders*"}, Exclude: []string{"health"}})
				if err != nil {
					b.Fatal(err)
				}
				for i := range spans {
					id := fmt.Sprintf("s%d", i)
					name := "work"
					switch {
					case i%50 == 0 && i/50%2 == 0:
						name = "orders"
					case i%50 == 0:
						name = "health"
					}
					f.Observe(domain.SpanUpserted{SpanID: id, Name: name})
					if i%50 > 0 {
						f.Observe(domain.SpanParentLinked{SpanID: id, ParentSpanID: fmt.Sprintf("s%d", i-1-i%3)})
					}
					f.Observe(domain.LogReceived{ID: "L" + id, SpanID: id})
				}
			}
		})
	}
}
