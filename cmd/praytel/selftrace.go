package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/barry-hennessy/bones/observability"
	bonesotel "github.com/barry-hennessy/bones/observability/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// errNoSelfTraceEndpoint is --self-trace without an outer praytel to send
// to. Praytel never guesses an endpoint: the default one could be its own
// receiver.
var errNoSelfTraceEndpoint = errors.New("--self-trace needs OTEL_EXPORTER_OTLP_ENDPOINT, as set by an outer praytel: praytel --tui -- praytel --self-trace -- command")

// newSelfTracer exports praytel's own spans, as service praytel, to the
// OTLP/gRPC endpoint praytel inherited -- for developing praytel under
// praytel. It is only ever built for --self-trace, so in regular use
// praytel's spans go nowhere. shutdown flushes what is still buffered.
func newSelfTracer(ctx context.Context, endpoint string) (observability.Tracer, func(context.Context) error, error) {
	if endpoint == "" {
		return nil, nil, errNoSelfTraceEndpoint
	}
	exp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(endpoint))
	if err != nil {
		return nil, nil, fmt.Errorf("self-trace exporter: %w", err)
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(attribute.String("service.name", "praytel")))
	if err != nil {
		return nil, nil, fmt.Errorf("self-trace resource: %w", err)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
	return bonesotel.New(tp.Tracer("praytel")), tp.Shutdown, nil
}
