package domain

// Event is a value-copy message from the correlator to renderers.
// See ARCHITECTURE.md.
type Event interface {
	isEvent()
}

type (
	SpanUpserted struct {
		TraceID   string
		SpanID    string
		Name      string
		Synthetic bool
	}
	SpanParentLinked struct {
		TraceID      string
		SpanID       string
		ParentSpanID string
	}
	SpanClosed struct {
		TraceID string
		SpanID  string
	}
	LogReceived struct {
		ID        string
		Body      string
		Source    Source
		TraceID   string
		SpanID    string
		SpanName  string
		Synthetic bool
	}
	LogAssigned struct {
		ID        string
		TraceID   string
		SpanID    string
		SpanName  string
		Synthetic bool
	}
	DrainComplete struct{}
	ForestEvicted struct{}
	ForestShed    struct{}
)

func (SpanUpserted) isEvent()     {}
func (SpanParentLinked) isEvent() {}
func (SpanClosed) isEvent()       {}
func (LogReceived) isEvent()      {}
func (LogAssigned) isEvent()      {}
func (DrainComplete) isEvent()    {}
func (ForestEvicted) isEvent()    {}
func (ForestShed) isEvent()       {}
