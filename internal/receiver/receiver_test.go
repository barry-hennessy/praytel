package receiver_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/receiver"
	"github.com/barry-hennessy/test/sweet"
	colllog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collmetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	colltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type recvDeps struct {
	r     *receiver.Receiver
	spans chan domain.Span
	logs  chan domain.LogLine
}

func recvFactory(t *testing.T) recvDeps {
	t.Helper()

	return startRecv(t, nil, nil)
}

func startRecv(t *testing.T, spans chan domain.Span, logs chan domain.LogLine) recvDeps {
	t.Helper()

	r, err := receiver.Listen(observability.LoggingTracer{}, receiver.Ports{GRPC: 0, HTTP: 0}, spans, logs)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	g, _ := errgroup.WithContext(ctx)
	g.Go(func() error {
		return r.Serve(ctx)
	})

	t.Cleanup(func() {
		cancel()
		if err := g.Wait(); err != nil {
			t.Errorf("serve: %v", err)
		}
	})

	return recvDeps{r: r, spans: spans, logs: logs}
}

func TestChildEnvPointsAtBoundPorts(t *testing.T) {
	sweet.Run(t, "ChildEnv names bound grpc and http ports", recvFactory, func(t *testing.T, d recvDeps) {
		env := strings.Join(d.r.ChildEnv(), "\n")
		grpcPort := strconv.Itoa(d.r.GRPCPort())
		httpPort := strconv.Itoa(d.r.HTTPPort())

		if !strings.Contains(env, grpcPort) {
			t.Fatalf("grpc port %s missing from env:\n%s", grpcPort, env)
		}
		if !strings.Contains(env, httpPort) {
			t.Fatalf("http port %s missing from env:\n%s", httpPort, env)
		}

		for _, line := range d.r.ChildEnv() {
			if !strings.HasPrefix(line, "OTEL_EXPORTER_OTLP_") {
				t.Fatalf("non-otlp exporter key: %s", line)
			}
			if strings.Contains(line, "METRIC") {
				t.Fatalf("metrics env set: %s", line)
			}
		}
	})
}

func TestNoMetricsHTTP(t *testing.T) {
	sweet.Run(t, "HTTP has no metrics surface", recvFactory, func(t *testing.T, d recvDeps) {
		url := "http://127.0.0.1:" + strconv.Itoa(d.r.HTTPPort()) + "/v1/metrics"
		res, err := http.Post(url, "application/x-protobuf", strings.NewReader(""))
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, res.Body)

		if res.StatusCode == http.StatusOK {
			t.Fatalf("metrics endpoint succeeded: %d", res.StatusCode)
		}
	})
}

func TestNoMetricsGRPC(t *testing.T) {
	sweet.Run(t, "gRPC has no metrics service", recvFactory, func(t *testing.T, d recvDeps) {
		conn, err := grpc.NewClient(
			"127.0.0.1:"+strconv.Itoa(d.r.GRPCPort()),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err = collmetric.NewMetricsServiceClient(conn).Export(ctx, &collmetric.ExportMetricsServiceRequest{})
		if err == nil {
			t.Fatal("metrics Export succeeded")
		}
		if status.Code(err) != codes.Unimplemented {
			t.Fatalf("want Unimplemented, got %v", err)
		}
	})
}

func TestGRPCTracesBecomeDomainSpans(t *testing.T) {
	sweet.Run(t, "gRPC Export delivers domain spans", func(t *testing.T) recvDeps {
		t.Helper()

		return startRecv(t, make(chan domain.Span, 4), nil)
	}, func(t *testing.T, d recvDeps) {
		conn, err := grpc.NewClient(
			"127.0.0.1:"+strconv.Itoa(d.r.GRPCPort()),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		traceID := bytes.Repeat([]byte{0xab}, 16)
		spanID := bytes.Repeat([]byte{0xcd}, 8)
		parentID := bytes.Repeat([]byte{0xef}, 8)
		start := uint64(1_000_000_000)
		end := start + uint64(25*time.Millisecond)

		_, err = colltrace.NewTraceServiceClient(conn).Export(ctx, &colltrace.ExportTraceServiceRequest{
			ResourceSpans: []*tracev1.ResourceSpans{{
				Resource: &resourcev1.Resource{
					Attributes: []*commonv1.KeyValue{{
						Key: "service.name",
						Value: &commonv1.AnyValue{
							Value: &commonv1.AnyValue_StringValue{StringValue: "helper-svc"},
						},
					}},
				},
				ScopeSpans: []*tracev1.ScopeSpans{{
					Spans: []*tracev1.Span{{
						TraceId:           traceID,
						SpanId:            spanID,
						ParentSpanId:      parentID,
						Name:              "unit-span",
						StartTimeUnixNano: start,
						EndTimeUnixNano:   end,
						Status:            &tracev1.Status{Code: tracev1.Status_STATUS_CODE_ERROR},
						Attributes: []*commonv1.KeyValue{{
							Key: "http.method",
							Value: &commonv1.AnyValue{
								Value: &commonv1.AnyValue_StringValue{StringValue: "GET"},
							},
						}},
					}},
				}},
			}},
		})
		if err != nil {
			t.Fatalf("export: %v", err)
		}

		select {
		case sp := <-d.spans:
			if sp.Name != "unit-span" {
				t.Fatalf("name=%q", sp.Name)
			}
			if sp.TraceID != hex.EncodeToString(traceID) {
				t.Fatalf("trace=%q", sp.TraceID)
			}
			if sp.SpanID != hex.EncodeToString(spanID) {
				t.Fatalf("span=%q", sp.SpanID)
			}
			if sp.ParentSpanID != hex.EncodeToString(parentID) {
				t.Fatalf("parent=%q", sp.ParentSpanID)
			}
			if !sp.Ended {
				t.Fatal("exported span should be ended")
			}
			if sp.Component != "helper-svc" {
				t.Fatalf("component=%q", sp.Component)
			}
			if sp.StartTime.UnixNano() != int64(start) || sp.EndTime.UnixNano() != int64(end) {
				t.Fatalf("times=%s..%s", sp.StartTime, sp.EndTime)
			}
			if len(sp.Attributes) != 1 || sp.Attributes[0].Key != "http.method" || sp.Attributes[0].Value != "GET" {
				t.Fatalf("attributes=%#v", sp.Attributes)
			}
			if sp.Status != domain.SpanStatusError {
				t.Fatalf("status=%v", sp.Status)
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for domain span")
		}
	})
}

func TestGRPCTraceArrayAttributesRenderTheirValues(t *testing.T) {
	sweet.Run(t, "StringSlice-kind (and other array) attributes render, not blank", func(t *testing.T) recvDeps {
		t.Helper()

		return startRecv(t, make(chan domain.Span, 4), nil)
	}, func(t *testing.T, d recvDeps) {
		conn, err := grpc.NewClient(
			"127.0.0.1:"+strconv.Itoa(d.r.GRPCPort()),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		traceID := bytes.Repeat([]byte{0xab}, 16)
		spanID := bytes.Repeat([]byte{0xcd}, 8)

		_, err = colltrace.NewTraceServiceClient(conn).Export(ctx, &colltrace.ExportTraceServiceRequest{
			ResourceSpans: []*tracev1.ResourceSpans{{
				ScopeSpans: []*tracev1.ScopeSpans{{
					Spans: []*tracev1.Span{{
						TraceId:         traceID,
						SpanId:          spanID,
						Name:            "array-attr-span",
						EndTimeUnixNano: 1,
						Attributes: []*commonv1.KeyValue{{
							Key: "ids",
							Value: &commonv1.AnyValue{
								Value: &commonv1.AnyValue_ArrayValue{
									ArrayValue: &commonv1.ArrayValue{
										Values: []*commonv1.AnyValue{
											{Value: &commonv1.AnyValue_StringValue{StringValue: "appointment_id:abc-123"}},
											{Value: &commonv1.AnyValue_StringValue{StringValue: "account_id:acc-456"}},
										},
									},
								},
							},
						}},
					}},
				}},
			}},
		})
		if err != nil {
			t.Fatalf("export: %v", err)
		}

		select {
		case sp := <-d.spans:
			if len(sp.Attributes) != 1 || sp.Attributes[0].Key != "ids" {
				t.Fatalf("attributes=%#v", sp.Attributes)
			}
			want := "[appointment_id:abc-123, account_id:acc-456]"
			if sp.Attributes[0].Value != want {
				t.Fatalf("array attribute value=%q, want %q", sp.Attributes[0].Value, want)
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for domain span")
		}
	})
}

func TestHTTPTracesBecomeDomainSpans(t *testing.T) {
	sweet.Run(t, "HTTP /v1/traces delivers domain spans", func(t *testing.T) recvDeps {
		t.Helper()

		return startRecv(t, make(chan domain.Span, 4), nil)
	}, func(t *testing.T, d recvDeps) {
		traceID := bytes.Repeat([]byte{0x11}, 16)
		spanID := bytes.Repeat([]byte{0x22}, 8)
		parentID := bytes.Repeat([]byte{0x33}, 8)

		body, err := proto.Marshal(&colltrace.ExportTraceServiceRequest{
			ResourceSpans: []*tracev1.ResourceSpans{{
				ScopeSpans: []*tracev1.ScopeSpans{{
					Spans: []*tracev1.Span{{
						TraceId:         traceID,
						SpanId:          spanID,
						ParentSpanId:    parentID,
						Name:            "http-span",
						EndTimeUnixNano: 1,
					}},
				}},
			}},
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var gz bytes.Buffer
		w := gzip.NewWriter(&gz)
		if _, err := w.Write(body); err != nil {
			t.Fatalf("gzip: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("gzip close: %v", err)
		}

		url := "http://127.0.0.1:" + strconv.Itoa(d.r.HTTPPort()) + "/v1/traces"
		req, err := http.NewRequest(http.MethodPost, url, &gz)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		req.Header.Set("Content-Type", "application/x-protobuf")
		req.Header.Set("Content-Encoding", "gzip")

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, res.Body)

		if res.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", res.StatusCode)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		select {
		case sp := <-d.spans:
			if sp.Name != "http-span" {
				t.Fatalf("name=%q", sp.Name)
			}
			if sp.TraceID != hex.EncodeToString(traceID) {
				t.Fatalf("trace=%q", sp.TraceID)
			}
			if sp.SpanID != hex.EncodeToString(spanID) {
				t.Fatalf("span=%q", sp.SpanID)
			}
			if sp.ParentSpanID != hex.EncodeToString(parentID) {
				t.Fatalf("parent=%q", sp.ParentSpanID)
			}
			if !sp.Ended {
				t.Fatal("exported http span should be ended")
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for http domain span")
		}
	})
}

func TestOTLPLogsBecomeLogLines(t *testing.T) {
	sweet.Run(t, "gRPC and HTTP logs become LogLine with source otlp", func(t *testing.T) recvDeps {
		t.Helper()

		return startRecv(t, nil, make(chan domain.LogLine, 4))
	}, func(t *testing.T, d recvDeps) {
		traceID := bytes.Repeat([]byte{0x44}, 16)
		spanID := bytes.Repeat([]byte{0x55}, 8)
		req := &colllog.ExportLogsServiceRequest{
			ResourceLogs: []*logsv1.ResourceLogs{{
				Resource: &resourcev1.Resource{
					Attributes: []*commonv1.KeyValue{{
						Key: "service.name",
						Value: &commonv1.AnyValue{
							Value: &commonv1.AnyValue_StringValue{StringValue: "helper-svc"},
						},
					}},
				},
				ScopeLogs: []*logsv1.ScopeLogs{{
					LogRecords: []*logsv1.LogRecord{{
						TraceId: traceID,
						SpanId:  spanID,
						Body: &commonv1.AnyValue{
							Value: &commonv1.AnyValue_StringValue{StringValue: "otlp-body"},
						},
					}},
				}},
			}},
		}

		conn, err := grpc.NewClient(
			"127.0.0.1:"+strconv.Itoa(d.r.GRPCPort()),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if _, err := colllog.NewLogsServiceClient(conn).Export(ctx, req); err != nil {
			t.Fatalf("grpc logs: %v", err)
		}

		line := recvLog(t, ctx, d.logs)
		if line.Source != domain.SourceOTLP {
			t.Fatalf("source=%q", line.Source)
		}
		if line.Body != "otlp-body" {
			t.Fatalf("body=%q", line.Body)
		}
		if line.Component != "helper-svc" {
			t.Fatalf("component=%q", line.Component)
		}
		if line.TraceID != hex.EncodeToString(traceID) || line.SpanID != hex.EncodeToString(spanID) {
			t.Fatalf("ids %#v", line)
		}

		body, err := proto.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		url := "http://127.0.0.1:" + strconv.Itoa(d.r.HTTPPort()) + "/v1/logs"
		res, err := http.Post(url, "application/x-protobuf", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, res.Body)

		if res.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", res.StatusCode)
		}

		httpLine := recvLog(t, ctx, d.logs)
		if httpLine.Source != domain.SourceOTLP || httpLine.Component != "helper-svc" || httpLine.Body != "otlp-body" {
			t.Fatalf("http log %#v", httpLine)
		}
	})
}

func TestTraceAndLogServicesExist(t *testing.T) {
	sweet.Run(t, "gRPC trace and log export succeed", recvFactory, func(t *testing.T, d recvDeps) {
		conn, err := grpc.NewClient(
			"127.0.0.1:"+strconv.Itoa(d.r.GRPCPort()),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if _, err := colltrace.NewTraceServiceClient(conn).Export(ctx, &colltrace.ExportTraceServiceRequest{}); err != nil {
			t.Fatalf("traces: %v", err)
		}
		if _, err := colllog.NewLogsServiceClient(conn).Export(ctx, &colllog.ExportLogsServiceRequest{}); err != nil {
			t.Fatalf("logs: %v", err)
		}
	})
}

func TestListenOccupied(t *testing.T) {
	sweet.Run(t, "occupied grpc port fails listen", func(t *testing.T) recvDeps { return recvDeps{} }, func(t *testing.T, _ recvDeps) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("hold port: %v", err)
		}
		defer ln.Close()

		port := ln.Addr().(*net.TCPAddr).Port
		_, err = receiver.Listen(observability.LoggingTracer{}, receiver.Ports{GRPC: port, HTTP: 0}, nil, nil)
		if err == nil {
			t.Fatal("expected listen error")
		}
		if !errors.Is(err, receiver.ErrListen) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestDropWarnsAndDoesNotEnqueue(t *testing.T) {
	sweet.Run(t, "Drop accepts export without enqueueing", func(t *testing.T) recvDeps {
		t.Helper()

		return startRecv(t, make(chan domain.Span, 1), nil)
	}, func(t *testing.T, d recvDeps) {
		d.r.Drop()

		conn, err := grpc.NewClient(
			"127.0.0.1:"+strconv.Itoa(d.r.GRPCPort()),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err = colltrace.NewTraceServiceClient(conn).Export(ctx, &colltrace.ExportTraceServiceRequest{
			ResourceSpans: []*tracev1.ResourceSpans{{
				ScopeSpans: []*tracev1.ScopeSpans{{
					Spans: []*tracev1.Span{{
						TraceId: bytes.Repeat([]byte{0xab}, 16),
						SpanId:  bytes.Repeat([]byte{0xcd}, 8),
						Name:    "after-drop",
					}},
				}},
			}},
		})
		if err != nil {
			t.Fatalf("export after drop: %v", err)
		}

		select {
		case sp := <-d.spans:
			t.Fatalf("enqueued after drop: %#v", sp)
		case <-time.After(50 * time.Millisecond):
		}
	})
}

func TestHTTPTracesRejectNonPOST(t *testing.T) {
	sweet.Run(t, "GET /v1/traces is method not allowed", recvFactory, func(t *testing.T, d recvDeps) {
		url := "http://127.0.0.1:" + strconv.Itoa(d.r.HTTPPort()) + "/v1/traces"
		res, err := http.Get(url)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, res.Body)

		if res.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("status=%d", res.StatusCode)
		}
	})
}

func TestHTTPTracesRejectOversizedBody(t *testing.T) {
	sweet.Run(t, "oversized POST is bad request", recvFactory, func(t *testing.T, d recvDeps) {
		url := "http://127.0.0.1:" + strconv.Itoa(d.r.HTTPPort()) + "/v1/traces"
		body := bytes.Repeat([]byte{'x'}, 4<<20+1)
		res, err := http.Post(url, "application/x-protobuf", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, res.Body)

		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("status=%d", res.StatusCode)
		}
	})
}

func recvLog(t *testing.T, ctx context.Context, logs <-chan domain.LogLine) domain.LogLine {
	t.Helper()

	select {
	case line := <-logs:
		return line
	case <-ctx.Done():
		t.Fatal("timed out waiting for log line")
		return domain.LogLine{}
	}
}
