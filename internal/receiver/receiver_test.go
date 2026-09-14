package receiver_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/receiver"
	"github.com/barry-hennessy/test/sweet"
	colllog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collmetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	colltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type recvDeps struct {
	r *receiver.Receiver
}

func recvFactory(t *testing.T) recvDeps {
	t.Helper()

	r, err := receiver.Listen(observability.LoggingTracer{}, receiver.Ports{GRPC: 0, HTTP: 0})
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

	return recvDeps{r: r}
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

func TestTraceAndLogServicesExist(t *testing.T) {
	sweet.Run(t, "gRPC trace and log export discard", recvFactory, func(t *testing.T, d recvDeps) {
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
		_, err = receiver.Listen(observability.LoggingTracer{}, receiver.Ports{GRPC: port, HTTP: 0})
		if err == nil {
			t.Fatal("expected listen error")
		}
		if !errors.Is(err, receiver.ErrListen) {
			t.Fatalf("err=%v", err)
		}
	})
}
