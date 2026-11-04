// Package testtrace lets a package's tests export OpenTelemetry spans to
// the OTLP endpoint praytel provides, so a test run can be read, or stamped
// as a golden, with praytel:
//
//	praytel --snapshot=tree -- go test -count=1 -run '^TestX$' .
//
// It does nothing unless OTEL_EXPORTER_OTLP_ENDPOINT is set, so a plain
// go test is unaffected. Use it in three places:
//
//	func TestMain(m *testing.M) { testtrace.Main(m) }  // flushes on exit
//	lt := testtrace.LoggingTracer(t)                   // spans from the code under test
//	ctx := testtrace.Context(t)                        // roots them under test.<Name>
package testtrace

import (
	"context"
	"os"
	"testing"

	"github.com/barry-hennessy/bones/observability"
	obsotel "github.com/barry-hennessy/bones/observability/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const scope = "praytel/testtrace"

var provider *sdktrace.TracerProvider

// Main runs the tests and exits. When an endpoint is configured it exports
// spans there and flushes them before exiting: a binary that exits without
// flushing loses its last spans.
func Main(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		exp, err := otlptracegrpc.New(context.Background(), otlptracegrpc.WithInsecure())
		if err != nil {
			panic("testtrace: " + err.Error())
		}

		// A fixed service name: the default is derived from the test
		// binary's name, which would put it in every golden.
		res := resource.NewSchemaless(attribute.String("service.name", "praytel"))
		provider = sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
		defer func() { _ = provider.Shutdown(context.Background()) }()
	}

	return m.Run()
}

// LoggingTracer returns a tracer for the code under test. Without an
// endpoint it is the zero value, which traces nothing.
func LoggingTracer(t testing.TB) observability.LoggingTracer {
	t.Helper()

	if provider == nil {
		return observability.LoggingTracer{}
	}

	return observability.LoggingTracer{Tracer: obsotel.New(provider.Tracer(scope))}
}

// Context returns the test's context carrying a root span named
// test.<Name> with a test.name attribute, ended when the test finishes.
// Spans started from this context nest under it. Without an endpoint it is
// t.Context().
func Context(t testing.TB) context.Context {
	t.Helper()

	if provider == nil {
		return t.Context()
	}

	ctx, span := provider.Tracer(scope).Start(t.Context(), "test."+t.Name())
	span.SetAttributes(attribute.String("test.name", t.Name()))
	t.Cleanup(func() { span.End() })

	return ctx
}
