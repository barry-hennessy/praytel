package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	exit := flag.Int("exit", 0, "exit code")
	stdout := flag.String("stdout", "", "bytes to write to stdout")
	stderr := flag.String("stderr", "", "bytes to write to stderr")
	marker := flag.String("marker", "", "create this file on start")
	printOtel := flag.Bool("print-otel-env", false, "print OTEL_EXPORTER_OTLP_* assignments")
	exportGRPC := flag.Bool("export-grpc-span", false, "export one OTLP/gRPC span and print JSON with those IDs")
	exportGRPCLogfmt := flag.Bool("export-grpc-span-logfmt", false, "export one OTLP/gRPC span and print logfmt with those IDs")
	exportHTTP := flag.Bool("export-http-span", false, "export one OTLP HTTP/protobuf span and print JSON with those IDs")
	exportOTLPLog := flag.String("export-otlp-log", "", "emit this body as an OTLP log")
	serviceName := flag.String("service-name", "", "resource service.name for OTLP logs")
	spanName := flag.String("span-name", "child-work", "name of the exported helper span")
	delay := flag.Duration("delay", 0, "sleep before in-process OTLP export")
	detachAfter := flag.Duration("detach-export-after", 0, "print JSON, spawn delayed exporter, exit immediately")
	keepExporting := flag.Bool("keep-exporting", false, "spawn a looping OTLP exporter and exit")
	loopExport := flag.Bool("loop-export-grpc", false, "export gRPC spans until killed")
	traceID := flag.String("trace-id", "", "hex trace id for exported span")
	spanID := flag.String("span-id", "", "hex span id for exported span")
	waitSignal := flag.Bool("wait-signal", false, "handle SIGINT/SIGTERM, print a JSON line, then exit")
	flag.Parse()

	if *marker != "" && !*waitSignal {
		if err := os.WriteFile(*marker, []byte("ran\n"), 0o600); err != nil {
			fmt.Fprint(os.Stderr, err.Error())
			os.Exit(1)
		}
	}

	fmt.Fprint(os.Stdout, *stdout)
	fmt.Fprint(os.Stderr, *stderr)

	if *printOtel {
		var lines []string
		for _, e := range os.Environ() {
			if strings.HasPrefix(e, "OTEL_EXPORTER_OTLP_") {
				lines = append(lines, e)
			}
		}
		sort.Strings(lines)
		for _, line := range lines {
			fmt.Fprintln(os.Stdout, line)
		}
	}

	if *loopExport {
		fails := 0
		for {
			if err := exportGRPCSpan(*spanName, *traceID, *spanID, false); err != nil {
				fails++
				if fails > 15 {
					os.Exit(0)
				}
				time.Sleep(20 * time.Millisecond)
				continue
			}
			fails = 0
			time.Sleep(10 * time.Millisecond)
		}
	}

	if *keepExporting {
		if err := detach("-loop-export-grpc", "-span-name", *spanName); err != nil {
			fmt.Fprint(os.Stderr, err.Error())
			os.Exit(1)
		}
		os.Exit(*exit)
	}

	if *detachAfter > 0 {
		tid, sid, err := idsOrRandom(*traceID, *spanID)
		if err != nil {
			fmt.Fprint(os.Stderr, err.Error())
			os.Exit(1)
		}

		fmt.Fprintf(os.Stdout, `{"msg":"grpc-span","trace_id":%q,"span_id":%q}`+"\n", tid, sid)
		if err := detach(
			"-export-grpc-span",
			"-span-name", *spanName,
			"-trace-id", tid,
			"-span-id", sid,
			"-delay", detachAfter.String(),
		); err != nil {
			fmt.Fprint(os.Stderr, err.Error())
			os.Exit(1)
		}
		os.Exit(*exit)
	}

	if *delay > 0 {
		time.Sleep(*delay)
	}

	if *exportGRPC || *exportGRPCLogfmt {
		if err := exportGRPCSpan(*spanName, *traceID, *spanID, *exportGRPCLogfmt); err != nil {
			fmt.Fprint(os.Stderr, err.Error())
			os.Exit(1)
		}
	}

	if *exportHTTP {
		if err := exportHTTPSpan(*spanName, *traceID, *spanID); err != nil {
			fmt.Fprint(os.Stderr, err.Error())
			os.Exit(1)
		}
	}

	if *exportOTLPLog != "" {
		if err := exportOTLPLogBody(*exportOTLPLog, *serviceName); err != nil {
			fmt.Fprint(os.Stderr, err.Error())
			os.Exit(1)
		}
	}

	if *waitSignal {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		if *marker != "" {
			if err := os.WriteFile(*marker, []byte("ran\n"), 0o600); err != nil {
				fmt.Fprint(os.Stderr, err.Error())
				os.Exit(1)
			}
		}
		sig := <-ch
		line, err := json.Marshal(map[string]string{"signal": sig.String()})
		if err != nil {
			fmt.Fprint(os.Stderr, err.Error())
			os.Exit(1)
		}
		line = append(line, '\n')
		if _, err := os.Stdout.Write(line); err != nil {
			fmt.Fprint(os.Stderr, err.Error())
			os.Exit(1)
		}
		if *marker != "" {
			if err := os.WriteFile(*marker, line, 0o600); err != nil {
				fmt.Fprint(os.Stderr, err.Error())
				os.Exit(1)
			}
		}
	}

	os.Exit(*exit)
}

func exportGRPCSpan(name, traceID, spanID string, logfmtOut bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithInsecure())
	if err != nil {
		return fmt.Errorf("otlp grpc exporter: %w", err)
	}

	return exportSpan(ctx, name, "grpc-span", traceID, spanID, exp, logfmtOut)
}

func exportHTTPSpan(name, traceID, spanID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Configured from the environment alone, like a child that hard-codes the
	// HTTP exporter: it ignores OTEL_EXPORTER_OTLP_PROTOCOL and posts to
	// OTEL_EXPORTER_OTLP_ENDPOINT, which names praytel's gRPC port.
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return fmt.Errorf("otlp http exporter: %w", err)
	}

	return exportSpan(ctx, name, "http-span", traceID, spanID, exp, false)
}

func exportSpan(ctx context.Context, name, msg, traceID, spanID string, exp sdktrace.SpanExporter, logfmtOut bool) error {
	opts := []sdktrace.TracerProviderOption{sdktrace.WithSyncer(exp)}
	gen, err := idGenerator(traceID, spanID)
	if err != nil {
		return err
	}
	if gen != nil {
		opts = append(opts, sdktrace.WithIDGenerator(gen))
	}

	tp := sdktrace.NewTracerProvider(opts...)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	_, span := tp.Tracer("praytel-helper").Start(ctx, name)
	sc := span.SpanContext()
	if logfmtOut {
		fmt.Fprintf(os.Stdout, "level=INFO msg=%q trace_id=%s span_id=%s\n", msg, sc.TraceID().String(), sc.SpanID().String())
	} else {
		fmt.Fprintf(os.Stdout, `{"msg":%q,"trace_id":%q,"span_id":%q}`+"\n", msg, sc.TraceID().String(), sc.SpanID().String())
	}
	span.End()

	if err := tp.ForceFlush(ctx); err != nil {
		return fmt.Errorf("flush traces: %w", err)
	}

	return nil
}

type fixedIDs struct {
	traceID trace.TraceID
	spanID  trace.SpanID
}

func (f fixedIDs) NewIDs(context.Context) (trace.TraceID, trace.SpanID) {
	return f.traceID, f.spanID
}

func (f fixedIDs) NewSpanID(context.Context, trace.TraceID) trace.SpanID {
	return f.spanID
}

func idGenerator(traceID, spanID string) (sdktrace.IDGenerator, error) {
	if traceID == "" && spanID == "" {
		return nil, nil
	}

	tid, sid, err := parseIDs(traceID, spanID)
	if err != nil {
		return nil, err
	}

	return fixedIDs{traceID: tid, spanID: sid}, nil
}

func parseIDs(traceID, spanID string) (trace.TraceID, trace.SpanID, error) {
	tid, err := trace.TraceIDFromHex(traceID)
	if err != nil {
		return trace.TraceID{}, trace.SpanID{}, fmt.Errorf("trace-id: %w", err)
	}

	sid, err := trace.SpanIDFromHex(spanID)
	if err != nil {
		return trace.TraceID{}, trace.SpanID{}, fmt.Errorf("span-id: %w", err)
	}

	return tid, sid, nil
}

func idsOrRandom(traceID, spanID string) (string, string, error) {
	if traceID != "" || spanID != "" {
		tid, sid, err := parseIDs(traceID, spanID)
		if err != nil {
			return "", "", err
		}

		return tid.String(), sid.String(), nil
	}

	tb := make([]byte, 16)
	sb := make([]byte, 8)
	if _, err := rand.Read(tb); err != nil {
		return "", "", err
	}
	if _, err := rand.Read(sb); err != nil {
		return "", "", err
	}

	return hex.EncodeToString(tb), hex.EncodeToString(sb), nil
}

func detach(args ...string) error {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}

	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = os.Environ()
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull

	if err := cmd.Start(); err != nil {
		_ = devNull.Close()
		return err
	}

	return nil
}

func exportOTLPLogBody(body, serviceName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	host, err := otlpHTTPHost()
	if err != nil {
		return err
	}

	exp, err := otlploghttp.New(ctx,
		otlploghttp.WithEndpoint(host),
		otlploghttp.WithURLPath("/v1/logs"),
		otlploghttp.WithInsecure(),
	)
	if err != nil {
		return fmt.Errorf("otlp log exporter: %w", err)
	}

	opts := []sdklog.LoggerProviderOption{
		sdklog.WithProcessor(sdklog.NewSimpleProcessor(exp)),
	}
	if serviceName != "" {
		opts = append(opts, sdklog.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		)))
	}

	lp := sdklog.NewLoggerProvider(opts...)
	defer func() { _ = lp.Shutdown(context.Background()) }()

	var rec otellog.Record
	rec.SetBody(otellog.StringValue(body))
	lp.Logger("praytel-helper").Emit(ctx, rec)

	if err := lp.ForceFlush(ctx); err != nil {
		return fmt.Errorf("flush logs: %w", err)
	}

	return nil
}

func otlpHTTPHost() (string, error) {
	ep := os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT")
	if ep == "" {
		return "", fmt.Errorf("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT unset")
	}

	u, err := url.Parse(ep)
	if err != nil {
		return "", fmt.Errorf("parse logs endpoint: %w", err)
	}

	if u.Host == "" {
		return "", fmt.Errorf("logs endpoint has no host: %q", ep)
	}

	return u.Host, nil
}
