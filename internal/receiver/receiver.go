package receiver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/barry-hennessy/bones"
	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
	colllog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	// OTLP exporters may gzip their requests, as the HTTP receiver already
	// accepts; registering the codec lets the gRPC server decode them too.
	_ "google.golang.org/grpc/encoding/gzip"
)

const (
	DefaultGRPCPort = 4317
	DefaultHTTPPort = 4318

	maxOTLPMessageBytes = 4 << 20
	maxOTLPStreams      = 256
	shutdownTimeout     = 3 * time.Second
)

// Errors returned by this package can be tested with [errors.Is].
var (
	ErrListen = errors.New("otlp listen")
)

type Ports struct {
	GRPC int
	HTTP int
}

// Receiver is an in-process OTLP listener (gRPC + HTTP traces and logs).
// Wire payloads are cast to [domain.Span] / [domain.LogLine] at this edge.
// There is no metrics surface.
type Receiver struct {
	lt observability.LoggingTracer

	grpcLis net.Listener
	httpLis net.Listener
	grpc    *grpc.Server
	http    *http.Server

	grpcPort int
	httpPort int

	drop *atomic.Bool
}

func Listen(lt observability.LoggingTracer, ports Ports, spans chan<- domain.Span, logs chan<- domain.LogLine) (*Receiver, error) {
	lt = lt.Name("receiver")

	grpcLis, err := net.Listen("tcp", listenAddr(ports.GRPC))
	if err != nil {
		return nil, fmt.Errorf("%w: grpc %d: %w", ErrListen, ports.GRPC, err)
	}

	httpLis, err := net.Listen("tcp", listenAddr(ports.HTTP))
	if err != nil {
		_ = grpcLis.Close()

		return nil, fmt.Errorf("%w: http %d: %w", ErrListen, ports.HTTP, err)
	}

	grpcPort, err := tcpPort(grpcLis)
	if err != nil {
		_ = grpcLis.Close()
		_ = httpLis.Close()

		return nil, err
	}

	httpPort, err := tcpPort(httpLis)
	if err != nil {
		_ = grpcLis.Close()
		_ = httpLis.Close()

		return nil, err
	}

	drop := &atomic.Bool{}
	traces := ingestTraces{
		lt:    lt,
		spans: spans,
		drop:  drop,
	}
	otlpLogs := ingestLogs{
		lt:   lt,
		logs: logs,
		drop: drop,
	}

	gs := grpc.NewServer(
		grpc.MaxRecvMsgSize(maxOTLPMessageBytes),
		grpc.MaxConcurrentStreams(maxOTLPStreams),
	)
	colltrace.RegisterTraceServiceServer(gs, traces)
	colllog.RegisterLogsServiceServer(gs, otlpLogs)

	mux := http.NewServeMux()
	mux.Handle("/v1/traces", traces)
	mux.Handle("/v1/logs", otlpLogs)

	hs := &http.Server{
		Handler:           limitHTTPInflight(mux, maxOTLPStreams),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return &Receiver{
		lt:       lt,
		grpcLis:  grpcLis,
		httpLis:  httpLis,
		grpc:     gs,
		http:     hs,
		grpcPort: grpcPort,
		httpPort: httpPort,
		drop:     drop,
	}, nil
}

// Drop warns and discards further OTLP instead of sending it into the correlator.
func (r *Receiver) Drop() {
	if r == nil || r.drop == nil {
		return
	}

	r.drop.Store(true)
}

func listenAddr(port int) string {
	return fmt.Sprintf("127.0.0.1:%d", port)
}

func tcpPort(lis net.Listener) (int, error) {
	ta, ok := lis.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("%w: %T", ErrListen, lis.Addr())
	}

	return ta.Port, nil
}

func (r *Receiver) GRPCPort() int {
	return r.grpcPort
}

func (r *Receiver) HTTPPort() int {
	return r.httpPort
}

// ChildEnv returns OTel-standard exporter keys pointing at this listener.
func (r *Receiver) ChildEnv() []string {
	grpcEP := fmt.Sprintf("http://127.0.0.1:%d", r.grpcPort)
	httpBase := fmt.Sprintf("http://127.0.0.1:%d", r.httpPort)

	return []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT=" + grpcEP,
		"OTEL_EXPORTER_OTLP_PROTOCOL=grpc",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=" + httpBase + "/v1/logs",
		"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf",
	}
}

func (r *Receiver) Serve(ctx context.Context) error {
	ctx, _, span := r.lt.Start(ctx)
	defer span.End()

	gg, ctx := errgroup.WithContext(ctx)

	gg.Go(func() error {
		err := r.grpc.Serve(r.grpcLis)
		if err == nil || errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}

		return err
	})
	gg.Go(bones.CloseWhenDone(ctx, func(context.Context) error {
		r.grpc.GracefulStop()
		return nil
	}))

	gg.Go(func() error {
		err := r.http.Serve(r.httpLis)
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return err
	})
	gg.Go(bones.CloseServerWhenDone(ctx, r.http, shutdownTimeout))

	return gg.Wait()
}

var (
	_ colltrace.TraceServiceServer = ingestTraces{}
	_ colllog.LogsServiceServer    = ingestLogs{}
	_ http.Handler                 = ingestTraces{}
	_ http.Handler                 = ingestLogs{}
)
