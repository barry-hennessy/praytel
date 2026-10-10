package main

import (
	"context"
	"os"
	"testing"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	testShop   *shop
	testTracer trace.Tracer
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	s, err := newShop(ctx)
	if err != nil {
		panic(err)
	}
	testShop = s
	if testTracer, err = s.tracer(ctx, "tests"); err != nil {
		panic(err)
	}
	code := m.Run()
	s.shutdown(ctx)
	os.Exit(code)
}

// run wraps a test in a span named after it, so each test is its own trace.
func run(t *testing.T, o order) error {
	t.Helper()
	ctx, span := testTracer.Start(context.Background(), t.Name())
	defer span.End()
	err := testShop.createOrder(ctx, o)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

func TestVisaOrderIsConfirmed(t *testing.T) {
	if err := run(t, order{"T-1", "ada", "visa", 1}); err != nil {
		t.Fatalf("order failed: %v", err)
	}
}

func TestAmexOrderIsConfirmed(t *testing.T) {
	if err := run(t, order{"T-2", "grace", "amex", 2}); err != nil {
		t.Fatalf("order failed: %v", err)
	}
}

// TestGiftCardOrderIsConfirmed fails on purpose: the demo shows praytel
// leading from the failing test down to the call that refused the card.
func TestGiftCardOrderIsConfirmed(t *testing.T) {
	if err := run(t, order{"T-3", "linus", "gift", 1}); err != nil {
		t.Fatalf("order failed: %v", err)
	}
}
