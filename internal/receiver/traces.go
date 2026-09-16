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
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
)

type ingestTraces struct {
	colltrace.UnimplementedTraceServiceServer

	lt    observability.LoggingTracer
	spans chan<- domain.Span
	drop  *atomic.Bool
}

func (s ingestTraces) Export(ctx context.Context, req *colltrace.ExportTraceServiceRequest) (*colltrace.ExportTraceServiceResponse, error) {
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
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	writeProtobuf(w, &colltrace.ExportTraceServiceResponse{})
}

func (s ingestTraces) dropping() bool {
	return s.drop != nil && s.drop.Load()
}

func (s ingestTraces) forward(ctx context.Context, req *colltrace.ExportTraceServiceRequest) error {
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
		for _, ss := range rs.GetScopeSpans() {
			if ss == nil {
				continue
			}

			for _, sp := range ss.GetSpans() {
				if sp == nil || len(sp.GetSpanId()) == 0 {
					continue
				}

				out = append(out, domain.Span{
					TraceID:      hex.EncodeToString(sp.GetTraceId()),
					SpanID:       hex.EncodeToString(sp.GetSpanId()),
					ParentSpanID: hex.EncodeToString(sp.GetParentSpanId()),
					Name:         sp.GetName(),
					Component:    component,
					StartTime:    unixNano(sp.GetStartTimeUnixNano()),
					EndTime:      unixNano(sp.GetEndTimeUnixNano()),
					Status:       spanStatus(sp.GetStatus().GetCode()),
					Ended:        sp.GetEndTimeUnixNano() != 0,
				})
			}
		}
	}

	return out
}

func unixNano(n uint64) time.Time {
	if n == 0 {
		return time.Time{}
	}

	return time.Unix(0, int64(n))
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
