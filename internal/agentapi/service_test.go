package agentapi

import (
	"reflect"
	"testing"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
)

func TestRichTelemetryConversionPreservesScopeEventsLinksAndTypes(t *testing.T) {
	attrs := []domain.Attr{{Key: "attempt", Value: domain.Int64Value(2)}}
	s := scope(domain.Scope{Name: "client", Version: "v1", Attributes: attrs})
	if s.Name != "client" || s.Version != "v1" || s.Attributes[0].Value.Data != int64(2) {
		t.Fatalf("scope=%+v", s)
	}

	at := time.Unix(10, 0)
	es := events([]domain.SpanEvent{{Name: "retry", Timestamp: at, Attributes: attrs, DroppedAttributesCount: 3}})
	ls := links([]domain.SpanLink{{TraceID: "trace", SpanID: "span", TraceState: "state", Attributes: attrs, DroppedAttributesCount: 4}})
	if len(es) != 1 || es[0].Timestamp != at || es[0].DroppedAttributesCount != 3 {
		t.Fatalf("events=%+v", es)
	}
	if len(ls) != 1 || ls[0].TraceState != "state" || ls[0].DroppedAttributesCount != 4 {
		t.Fatalf("links=%+v", ls)
	}

	v := value(domain.ArrayValue([]domain.Value{domain.StringValue("ready"), domain.BoolValue(true)}))
	want := []any{Value{Kind: "string", Display: "ready", Data: "ready"}, Value{Kind: "bool", Display: "true", Data: true}}
	if v.Kind != "array" || !reflect.DeepEqual(v.Data, want) {
		t.Fatalf("value=%+v", v)
	}
}
