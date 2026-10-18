package main_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/receiver"
)

// collector stands in for an outer praytel: an OTLP receiver whose
// endpoint the praytel under test inherits.
type collector struct {
	endpoint string
	mu       sync.Mutex
	spans    []domain.Span
}

func newCollector(t *testing.T) *collector {
	t.Helper()
	spans := make(chan domain.Span, 1024)
	logs := make(chan domain.LogLine, 1024)
	rec, err := receiver.Listen(observability.LoggingTracer{}, receiver.Ports{}, spans, logs)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = rec.Serve(ctx) }()
	c := &collector{}
	for _, kv := range rec.ChildEnv() {
		if v, ok := strings.CutPrefix(kv, "OTEL_EXPORTER_OTLP_ENDPOINT="); ok {
			c.endpoint = v
		}
	}
	go func() {
		defer close(done)
		for {
			select {
			case sp := <-spans:
				c.mu.Lock()
				c.spans = append(c.spans, sp)
				c.mu.Unlock()
			case <-logs:
			case <-ctx.Done():
				return
			}
		}
	}()
	t.Cleanup(func() { cancel(); <-done })
	return c
}

// received waits briefly for stragglers, then returns every span so far.
func (c *collector) received() []domain.Span {
	time.Sleep(200 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]domain.Span(nil), c.spans...)
}

func TestPraytelsSpansStayDarkWithoutSelfTrace(t *testing.T) {
	c := newCollector(t)
	res := runPraytelEnv(t, t.TempDir(), []string{"OTEL_EXPORTER_OTLP_ENDPOINT=" + c.endpoint},
		"--grpc-port", strconv.Itoa(mustFreePort(t)), "--http-port", strconv.Itoa(mustFreePort(t)),
		"--drain-idle", "5ms", "--", childBin, "-stdout", "hello")
	if res.exit != 0 {
		t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
	}
	if got := c.received(); len(got) != 0 {
		t.Fatalf("praytel exported %d spans of its own without --self-trace: %+v", len(got), got)
	}
}

func TestSelfTraceNeedsAnOuterPraytel(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "child-ran")
	res := runPraytelEnv(t, dir, []string{"OTEL_EXPORTER_OTLP_ENDPOINT="},
		"--self-trace", "--", childBin, "-marker", marker)
	if res.exit == 0 || !strings.Contains(res.stderr, "--self-trace needs OTEL_EXPORTER_OTLP_ENDPOINT") {
		t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the child ran although praytel could not trace itself")
	}
}

func TestSelfTraceExportsPraytelsOwnSpans(t *testing.T) {
	c := newCollector(t)
	// No ports given: a self-traced praytel must pick free ones rather than
	// fight its outer praytel for 4317/4318.
	res := runPraytelEnv(t, t.TempDir(), []string{"OTEL_EXPORTER_OTLP_ENDPOINT=" + c.endpoint},
		"--self-trace", "--drain-idle", "5ms", "--", childBin, "-print-otel-env")
	if res.exit != 0 {
		t.Fatalf("exit=%d stderr=%q", res.exit, res.stderr)
	}
	if strings.Contains(res.stdout, ":4317") || strings.Contains(res.stdout, ":4318") {
		t.Fatalf("self-traced praytel used the default ports: %q", res.stdout)
	}

	names := map[string]bool{}
	for _, sp := range c.received() {
		if sp.Component != "praytel" {
			t.Fatalf("span %q has service %q, want praytel", sp.Name, sp.Component)
		}
		names[sp.Name] = true
	}
	for _, want := range []string{"praytel", "correlator", "receiver", "capture", "stream"} {
		if !names[want] {
			t.Fatalf("no %q span among %v", want, names)
		}
	}
}
