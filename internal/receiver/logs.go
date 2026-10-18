package receiver

import (
	"context"
	"encoding/hex"
	"net/http"
	"sync/atomic"
	"time"

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
	ctx, log, span := s.lt.Name("receiver.grpc.logs").Start(ctx)
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
	ctx, log, span := s.lt.Name("receiver.http.logs").Start(req.Context())
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
		resource := domain.Resource{Attributes: attributes(rl.GetResource().GetAttributes())}
		for _, sl := range rl.GetScopeLogs() {
			if sl == nil {
				continue
			}

			scope := scopeFromProto(sl.GetScope())
			for _, rec := range sl.GetLogRecords() {
				if rec == nil {
					continue
				}

				out = append(out, domain.LogLine{
					Timestamp:         unixNano(rec.GetTimeUnixNano()),
					ObservedTimestamp: unixNano(rec.GetObservedTimeUnixNano()),
					SeverityNumber:    int32(rec.GetSeverityNumber()),
					SeverityText:      rec.GetSeverityText(),
					Body:              anyValue(rec.GetBody()),
					Resource:          resource,
					Scope:             scope,
					Attributes:        attributes(rec.GetAttributes()),
					Source:            domain.SourceOTLP,
					Component:         component,
					TraceID:           hex.EncodeToString(rec.GetTraceId()),
					SpanID:            hex.EncodeToString(rec.GetSpanId()),
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

		return anyValue(kv.GetValue()).String()
	}

	return ""
}

func anyValue(av *commonv1.AnyValue) domain.Value {
	if av == nil {
		return domain.Value{}
	}

	switch v := av.GetValue().(type) {
	case *commonv1.AnyValue_StringValue:
		return domain.StringValue(v.StringValue)
	case *commonv1.AnyValue_IntValue:
		return domain.Int64Value(v.IntValue)
	case *commonv1.AnyValue_DoubleValue:
		return domain.Float64Value(v.DoubleValue)
	case *commonv1.AnyValue_BoolValue:
		return domain.BoolValue(v.BoolValue)
	case *commonv1.AnyValue_BytesValue:
		return domain.BytesValue(v.BytesValue)
	case *commonv1.AnyValue_ArrayValue:
		vals := v.ArrayValue.GetValues()
		parts := make([]domain.Value, len(vals))
		for i, e := range vals {
			parts[i] = anyValue(e)
		}
		return domain.ArrayValue(parts)
	case *commonv1.AnyValue_KvlistValue:
		kvs := v.KvlistValue.GetValues()
		parts := make([]domain.Attr, 0, len(kvs))
		for i, kv := range kvs {
			if kv != nil {
				parts = append(parts, domain.Attr{Key: kv.GetKey(), Value: anyValue(kv.GetValue())})
			}
			_ = i
		}
		return domain.MapValue(parts)
	default:
		return domain.Value{}
	}
}

func unixNano(n uint64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, int64(n))
}

func attributes(kvs []*commonv1.KeyValue) []domain.Attr {
	out := make([]domain.Attr, 0, len(kvs))
	for _, kv := range kvs {
		if kv != nil && kv.GetKey() != "" {
			out = append(out, domain.Attr{Key: kv.GetKey(), Value: anyValue(kv.GetValue())})
		}
	}
	return out
}

func scopeFromProto(s *commonv1.InstrumentationScope) domain.Scope {
	if s == nil {
		return domain.Scope{}
	}
	return domain.Scope{Name: s.GetName(), Version: s.GetVersion(), Attributes: attributes(s.GetAttributes())}
}
