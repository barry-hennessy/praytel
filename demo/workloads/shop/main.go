// Command shop is a small instrumented workload for praytel's demo
// recordings. It places a few orders across four in-process "services",
// exporting OTLP traces and writing trace-tagged JSON logs to stdout.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type order struct {
	id, customer, card string
	items              int
}

// shop holds one tracer per service so each exports its own service.name.
type shop struct {
	log                             *slog.Logger
	web, checkout, inventory, payer trace.Tracer
	providers                       []*sdktrace.TracerProvider
}

func newShop(ctx context.Context) (*shop, error) {
	s := &shop{log: slog.New(slog.NewJSONHandler(os.Stdout, nil))}
	for name, dst := range map[string]*trace.Tracer{
		"web": &s.web, "checkout": &s.checkout, "inventory": &s.inventory, "payments": &s.payer,
	} {
		t, err := s.tracer(ctx, name)
		if err != nil {
			return nil, err
		}
		*dst = t
	}
	return s, nil
}

// tracer returns a tracer whose spans carry service.name = service. The
// exporter reads its endpoint from OTEL_EXPORTER_OTLP_*, which praytel sets.
//
// With no endpoint configured nothing is exported, so the app also runs
// cleanly on its own; spans are still created and logs still carry their IDs.
func (s *shop) tracer(ctx context.Context, service string) (trace.Tracer, error) {
	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", service))),
	}
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		exp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithInsecure())
		if err != nil {
			return nil, fmt.Errorf("exporter for %s: %w", service, err)
		}
		opts = append(opts, sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(100*time.Millisecond)))
	}
	tp := sdktrace.NewTracerProvider(opts...)
	s.providers = append(s.providers, tp)
	return tp.Tracer("shop"), nil
}

func (s *shop) shutdown(ctx context.Context) {
	for _, tp := range s.providers {
		_ = tp.Shutdown(ctx)
	}
}

// logAt writes a JSON log line tagged with the active trace and span IDs so
// praytel can attach it to the span that emitted it.
func (s *shop) logAt(ctx context.Context, level slog.Level, msg string, args ...any) {
	sc := trace.SpanContextFromContext(ctx)
	args = append(args, "trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
	s.log.Log(ctx, level, msg, args...)
}

func fail(span trace.Span, err error) error {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

func work(d time.Duration) { time.Sleep(d) }

func (s *shop) createOrder(ctx context.Context, o order) error {
	ctx, span := s.web.Start(ctx, "Orders.Create", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	span.SetAttributes(attribute.String("order.id", o.id), attribute.String("customer", o.customer))

	s.logAt(ctx, slog.LevelInfo, "order received", "order", o.id, "items", o.items)
	work(5 * time.Millisecond)
	if err := s.place(ctx, o); err != nil {
		s.logAt(ctx, slog.LevelError, "order rejected", "order", o.id, "reason", err.Error())
		return fail(span, err)
	}
	s.logAt(ctx, slog.LevelInfo, "order confirmed", "order", o.id)
	return nil
}

func (s *shop) place(ctx context.Context, o order) error {
	ctx, span := s.checkout.Start(ctx, "Orders.Place")
	defer span.End()

	if err := s.reserve(ctx, o); err != nil {
		return fail(span, err)
	}
	if err := s.charge(ctx, o); err != nil {
		s.release(ctx, o)
		return fail(span, err)
	}
	return s.save(ctx, o)
}

func (s *shop) reserve(ctx context.Context, o order) error {
	ctx, span := s.inventory.Start(ctx, "Stock.Reserve")
	defer span.End()
	span.SetAttributes(attribute.Int("items", o.items))

	_, lock := s.inventory.Start(ctx, "Stock.Repo.Lock", trace.WithSpanKind(trace.SpanKindClient))
	work(time.Duration(10*o.items) * time.Millisecond)
	lock.End()
	s.logAt(ctx, slog.LevelInfo, "stock reserved", "order", o.id, "items", o.items)
	return nil
}

func (s *shop) release(ctx context.Context, o order) {
	ctx, span := s.inventory.Start(ctx, "Stock.Release")
	defer span.End()
	work(8 * time.Millisecond)
	s.logAt(ctx, slog.LevelWarn, "stock released", "order", o.id)
}

func (s *shop) charge(ctx context.Context, o order) error {
	ctx, span := s.payer.Start(ctx, "Gateway.Charge")
	defer span.End()
	span.SetAttributes(attribute.String("card.type", o.card))

	if err := s.authorize(ctx, o); err != nil {
		return fail(span, err)
	}
	s.logAt(ctx, slog.LevelInfo, "payment captured", "order", o.id)
	return nil
}

// authorize is the outbound call to the bank: slow for "amex", refused for "gift".
func (s *shop) authorize(ctx context.Context, o order) error {
	ctx, span := s.payer.Start(ctx, "Gateway.Bank.Authorize", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	switch o.card {
	case "amex":
		s.logAt(ctx, slog.LevelWarn, "bank slow, retrying", "order", o.id, "attempt", 2)
		work(450 * time.Millisecond)
	case "gift":
		work(30 * time.Millisecond)
		err := errors.New("unsupported card type: gift")
		s.logAt(ctx, slog.LevelError, "authorization refused", "order", o.id, "card", o.card)
		return fail(span, err)
	default:
		work(60 * time.Millisecond)
	}
	return nil
}

func (s *shop) save(ctx context.Context, o order) error {
	_, span := s.checkout.Start(ctx, "Orders.Repo.Save", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	work(15 * time.Millisecond)
	return nil
}

func main() {
	pace := flag.Duration("pace", 700*time.Millisecond, "pause between orders")
	flag.Parse()

	ctx := context.Background()
	s, err := newShop(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println("shop: taking orders")
	orders := []order{
		{"A-1001", "ada", "visa", 2},
		{"A-1002", "grace", "amex", 1},
		{"A-1003", "linus", "gift", 3},
		{"A-1004", "margaret", "visa", 1},
	}
	for _, o := range orders {
		_ = s.createOrder(ctx, o)
		time.Sleep(*pace)
	}
	s.shutdown(ctx)
}
