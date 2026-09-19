package receiver

import (
	"context"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
	colllog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
)

type ingestLogs struct {
	colllog.UnimplementedLogsServiceServer

	lt   observability.LoggingTracer
	logs chan<- domain.LogLine
	drop *atomic.Bool
}

func (s ingestLogs) Export(ctx context.Context, req *colllog.ExportLogsServiceRequest) (*colllog.ExportLogsServiceResponse, error) {
	ctx, log, span := s.lt.Start(ctx)
	defer span.End()

	if s.dropping() {
		log.Warn("dropping otlp after drain")
		return &colllog.ExportLogsServiceResponse{}, nil
	}

	if err := s.forward(ctx, req); err != nil {
		return nil, err
	}

	return &colllog.ExportLogsServiceResponse{}, nil
}

func (s ingestLogs) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx, log, span := s.lt.Start(req.Context())
	defer span.End()

	if !requirePOST(w, req) {
		return
	}

	limitHTTPBody(w, req)

	if s.dropping() {
		log.Warn("dropping otlp after drain")
		writeProtobuf(w, &colllog.ExportLogsServiceResponse{})
		return
	}

	var in colllog.ExportLogsServiceRequest
	if err := readProtobuf(req, &in); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if err := s.forward(ctx, &in); err != nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	writeProtobuf(w, &colllog.ExportLogsServiceResponse{})
}

func (s ingestLogs) dropping() bool {
	return s.drop != nil && s.drop.Load()
}

func (s ingestLogs) forward(ctx context.Context, req *colllog.ExportLogsServiceRequest) error {
	if s.logs == nil {
		return nil
	}

	for _, line := range domainLogs(req) {
		if s.dropping() {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case s.logs <- line:
		}
	}

	return nil
}

func domainLogs(req *colllog.ExportLogsServiceRequest) []domain.LogLine {
	if req == nil {
		return nil
	}

	var out []domain.LogLine
	for _, rl := range req.GetResourceLogs() {
		if rl == nil {
			continue
		}

		component := resourceServiceName(rl.GetResource())
		for _, sl := range rl.GetScopeLogs() {
			if sl == nil {
				continue
			}

			for _, rec := range sl.GetLogRecords() {
				if rec == nil {
					continue
				}

				out = append(out, domain.LogLine{
					Body:      anyValueString(rec.GetBody()),
					Source:    domain.SourceOTLP,
					Component: component,
					TraceID:   hex.EncodeToString(rec.GetTraceId()),
					SpanID:    hex.EncodeToString(rec.GetSpanId()),
				})
			}
		}
	}

	return out
}

func resourceServiceName(res *resourcev1.Resource) string {
	if res == nil {
		return ""
	}

	for _, kv := range res.GetAttributes() {
		if kv.GetKey() != "service.name" {
			continue
		}

		return anyValueString(kv.GetValue())
	}

	return ""
}

func anyValueString(av *commonv1.AnyValue) string {
	if av == nil {
		return ""
	}

	switch v := av.GetValue().(type) {
	case *commonv1.AnyValue_StringValue:
		return v.StringValue
	case *commonv1.AnyValue_IntValue:
		return strconv.FormatInt(v.IntValue, 10)
	case *commonv1.AnyValue_DoubleValue:
		return strconv.FormatFloat(v.DoubleValue, 'g', -1, 64)
	case *commonv1.AnyValue_BoolValue:
		return strconv.FormatBool(v.BoolValue)
	case *commonv1.AnyValue_BytesValue:
		return hex.EncodeToString(v.BytesValue)
	case *commonv1.AnyValue_ArrayValue:
		vals := v.ArrayValue.GetValues()
		parts := make([]string, len(vals))
		for i, e := range vals {
			parts[i] = anyValueString(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *commonv1.AnyValue_KvlistValue:
		kvs := v.KvlistValue.GetValues()
		parts := make([]string, len(kvs))
		for i, kv := range kvs {
			parts[i] = kv.GetKey() + "=" + anyValueString(kv.GetValue())
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return av.GetStringValue()
	}
}
