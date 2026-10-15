package capture

import (
	"encoding/json"
	"strings"

	"github.com/barry-hennessy/praytel/internal/domain"
)

// fields are the parts of a structured line praytel keeps: the IDs that join
// it to a span, and its scalar values as attributes.
type fields struct {
	traceID string
	spanID  string
	attrs   []domain.Attr
}

func (f fields) joined() bool { return f.traceID != "" || f.spanID != "" }

// addID records val as the trace or span ID if key names one. The first ID of
// each kind in a line wins. The key names cover OpenTelemetry's logging
// bridges and the zap, slog, zerolog and logrus hooks (trace_id), Spring Boot
// and Micrometer (traceId), Go code that follows Go's initialism style
// (traceID), and the Elastic Common Schema (trace.id).
func (f *fields) addID(key, val string) {
	switch key {
	case "trace_id", "traceId", "traceID", "trace.id":
		if f.traceID == "" {
			f.traceID = val
		}
	case "span_id", "spanId", "spanID", "span.id":
		if f.spanID == "" {
			f.spanID = val
		}
	}
}

// decodeObject reads one JSON object from the start of s in a single pass. It
// returns the object's fields and the offset just past its closing brace.
// Nested objects and arrays are read but not kept.
func decodeObject(s string) (fields, int, bool) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return fields{}, 0, false
	}

	var f fields
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return fields{}, 0, false
		}
		key, _ := tok.(string)

		var v any
		if err := dec.Decode(&v); err != nil {
			return fields{}, 0, false
		}

		switch x := v.(type) {
		case string:
			f.addID(key, x)
			f.attrs = append(f.attrs, domain.Attr{Key: key, Value: domain.StringValue(x)})
		case bool:
			f.attrs = append(f.attrs, domain.Attr{Key: key, Value: domain.BoolValue(x)})
		case json.Number:
			if i, err := x.Int64(); err == nil {
				f.attrs = append(f.attrs, domain.Attr{Key: key, Value: domain.Int64Value(i)})
			} else if fl, err := x.Float64(); err == nil {
				f.attrs = append(f.attrs, domain.Attr{Key: key, Value: domain.Float64Value(fl)})
			}
		}
	}

	if _, err := dec.Token(); err != nil {
		return fields{}, 0, false
	}

	return f, int(dec.InputOffset()), true
}
