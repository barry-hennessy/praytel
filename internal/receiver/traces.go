package receiver

import (
	"context"
	"encoding/hex"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
	colltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
)

type ingestTraces struct {
	colltrace.UnimplementedTraceServiceServer

	lt    observability.LoggingTracer
	spans chan<- domain.Span
	drop  *atomic.Bool
}

func (s ingestTraces) Export(
	ctx context.Context,
	req *colltrace.ExportTraceServiceRequest,
) (*colltrace.ExportTraceServiceResponse, error) {
	ctx, log, span := s.lt.Start(ctx)
	defer span.End()

	if s.dropping() {
		log.Warn("dropping otlp after drain")
		return &colltrace.ExportTraceServiceResponse{}, nil
	}

	if err := s.forward(ctx, req); err != nil {
		return nil, err
	}

	return &colltrace.ExportTraceServiceResponse{}, nil
}

func (s ingestTraces) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx, log, span := s.lt.Start(req.Context())
	defer span.End()

	if !requirePOST(w, req) {
		return
	}

	limitHTTPBody(w, req)

	if s.dropping() {
		log.Warn("dropping otlp after drain")
		writeProtobuf(w, &colltrace.ExportTraceServiceResponse{})
		return
	}

	var in colltrace.ExportTraceServiceRequest
	if err := readProtobuf(req, &in); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if err := s.forward(ctx, &in); err != nil {
		http.Error(
			w,
			http.StatusText(http.StatusServiceUnavailable),
			http.StatusServiceUnavailable,
		)
		return
	}

	writeProtobuf(w, &colltrace.ExportTraceServiceResponse{})
}

func (s ingestTraces) dropping() bool {
	return s.drop != nil && s.drop.Load()
}

func (s ingestTraces) forward(
	ctx context.Context,
	req *colltrace.ExportTraceServiceRequest,
) error {
	if s.spans == nil {
		return nil
	}

	for _, ds := range domainSpans(req) {
		if s.dropping() {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case s.spans <- ds:
		}
	}

	return nil
}

func domainSpans(req *colltrace.ExportTraceServiceRequest) []domain.Span {
	if req == nil {
		return nil
	}

	var out []domain.Span
	for _, rs := range req.GetResourceSpans() {
		if rs == nil {
			continue
		}

		component := resourceServiceName(rs.GetResource())
		resource := domain.Resource{Attributes: attributes(rs.GetResource().GetAttributes())}
		for _, ss := range rs.GetScopeSpans() {
			if ss == nil {
				continue
			}

			scope := scopeFromProto(ss.GetScope())
			for _, sp := range ss.GetSpans() {
				if sp == nil || len(sp.GetSpanId()) == 0 {
					continue
				}
				var startTime, endTime time.Time
				if n := sp.GetStartTimeUnixNano(); n != 0 {
					startTime = time.Unix(0, int64(n))
				}
				if n := sp.GetEndTimeUnixNano(); n != 0 {
					endTime = time.Unix(0, int64(n))
				}

				out = append(out, domain.Span{
					TraceID:       hex.EncodeToString(sp.GetTraceId()),
					SpanID:        hex.EncodeToString(sp.GetSpanId()),
					ParentSpanID:  hex.EncodeToString(sp.GetParentSpanId()),
					Name:          sp.GetName(),
					Component:     component,
					StartTime:     startTime,
					EndTime:       endTime,
					Status:        spanStatus(sp.GetStatus().GetCode()),
					StatusMessage: sp.GetStatus().GetMessage(),
					Kind:          spanKind(sp.GetKind()),
					Resource:      resource,
					Scope:         scope,
					Attributes:    spanAttributes(sp.GetAttributes()),
					Events:        spanEvents(sp.GetEvents()),
					Links:         spanLinks(sp.GetLinks()),
					Ended:         sp.GetEndTimeUnixNano() != 0,
				})
			}
		}
	}

	return out
}

func spanAttributes(kvs []*commonv1.KeyValue) []domain.Attr {
	if len(kvs) == 0 {
		return nil
	}

	out := make([]domain.Attr, 0, len(kvs))
	for _, kv := range kvs {
		if kv == nil || kv.GetKey() == "" {
			continue
		}
		out = append(out, domain.Attr{Key: kv.GetKey(), Value: anyValue(kv.GetValue())})
	}
	return out
}

func spanKind(k tracev1.Span_SpanKind) domain.SpanKind { return domain.SpanKind(k) }

func spanEvents(in []*tracev1.Span_Event) []domain.SpanEvent {
	out := make([]domain.SpanEvent, 0, len(in))
	for _, e := range in {
		if e != nil {
			out = append(out, domain.SpanEvent{Name: e.GetName(), Timestamp: unixNano(e.GetTimeUnixNano()), Attributes: attributes(e.GetAttributes()), DroppedAttributesCount: e.GetDroppedAttributesCount()})
		}
	}
	return out
}

func spanLinks(in []*tracev1.Span_Link) []domain.SpanLink {
	out := make([]domain.SpanLink, 0, len(in))
	for _, l := range in {
		if l != nil {
			out = append(out, domain.SpanLink{TraceID: hex.EncodeToString(l.GetTraceId()), SpanID: hex.EncodeToString(l.GetSpanId()), TraceState: l.GetTraceState(), Attributes: attributes(l.GetAttributes()), DroppedAttributesCount: l.GetDroppedAttributesCount()})
		}
	}
	return out
}

func spanStatus(status tracev1.Status_StatusCode) domain.SpanStatus {
	switch status {
	case tracev1.Status_STATUS_CODE_OK:
		return domain.SpanStatusOK
	case tracev1.Status_STATUS_CODE_ERROR:
		return domain.SpanStatusError
	default:
		return domain.SpanStatusUnset
	}
}
