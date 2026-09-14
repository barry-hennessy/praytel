package receiver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/barry-hennessy/bones"
	"github.com/barry-hennessy/bones/observability"
	colllog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

const (
	DefaultGRPCPort = 4317
	DefaultHTTPPort = 4318

	shutdownTimeout = 3 * time.Second
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
// Payloads are discarded until ingest tickets land. There is no metrics surface.
type Receiver struct {
	lt observability.LoggingTracer

	grpcLis net.Listener
	httpLis net.Listener
	grpc    *grpc.Server
	http    *http.Server

	grpcPort int
	httpPort int
}

func Listen(lt observability.LoggingTracer, ports Ports) (*Receiver, error) {
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

	gs := grpc.NewServer()
	colltrace.RegisterTraceServiceServer(gs, discardTraces{})
	colllog.RegisterLogsServiceServer(gs, discardLogs{})

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", discardHTTP(&colltrace.ExportTraceServiceResponse{}))
	mux.HandleFunc("/v1/logs", discardHTTP(&colllog.ExportLogsServiceResponse{}))

	hs := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return &Receiver{
		lt:       lt,
		grpcLis:  grpcLis,
		httpLis:  httpLis,
		grpc:     gs,
		http:     hs,
		grpcPort: grpcPort,
		httpPort: httpPort,
	}, nil
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
	_ colltrace.TraceServiceServer = discardTraces{}
	_ colllog.LogsServiceServer    = discardLogs{}
)

type discardTraces struct {
	colltrace.UnimplementedTraceServiceServer
}

func (discardTraces) Export(context.Context, *colltrace.ExportTraceServiceRequest) (*colltrace.ExportTraceServiceResponse, error) {
	return &colltrace.ExportTraceServiceResponse{}, nil
}

type discardLogs struct {
	colllog.UnimplementedLogsServiceServer
}

func (discardLogs) Export(context.Context, *colllog.ExportLogsServiceRequest) (*colllog.ExportLogsServiceResponse, error) {
	return &colllog.ExportLogsServiceResponse{}, nil
}

func discardHTTP(resp proto.Message) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.Copy(io.Discard, req.Body)
		_ = req.Body.Close()

		body, err := proto.Marshal(resp)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}
