package query_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/query"
	queryrender "github.com/barry-hennessy/praytel/internal/render/query"
)

func TestNDJSONFinalEnvelope(t *testing.T) {
	q, err := query.Parse(`span{ span:status = error } | matches`)
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan domain.Event, 4)
	var out bytes.Buffer
	r := queryrender.New(events, &out, q, queryrender.Options{Format: queryrender.NDJSON})
	r.SetChildResult(7, true)
	events <- domain.SpanUpserted{TraceID: "t", SpanID: "s", Status: domain.SpanStatusError}
	events <- domain.SpanClosed{TraceID: "t", SpanID: "s"}
	events <- domain.DrainComplete{}
	close(events)
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode %q: %v", out.String(), err)
	}
	if got["type"] != "run_finished" || got["run_state"] != "final" || got["matched"] != float64(1) || got["child_exit_code"] != float64(7) {
		t.Fatalf("envelope: %#v", got)
	}
}

func TestSettledThenFinal(t *testing.T) {
	q, _ := query.Parse(`span{ span:name = work }`)
	events := make(chan domain.Event)
	var out bytes.Buffer
	r := queryrender.New(events, &out, q, queryrender.Options{Format: queryrender.NDJSON, Emit: queryrender.EmitSettled, SettleAfter: 10 * time.Millisecond})
	done := make(chan error, 1)
	go func() { done <- r.Run(context.Background()) }()
	events <- domain.SpanUpserted{TraceID: "t", SpanID: "s", Name: "work"}
	events <- domain.SpanClosed{TraceID: "t", SpanID: "s"}
	time.Sleep(30 * time.Millisecond)
	events <- domain.DrainComplete{}
	close(events)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	var first, second map[string]any
	if err := dec.Decode(&first); err != nil {
		t.Fatal(err)
	}
	if err := dec.Decode(&second); err != nil {
		t.Fatal(err)
	}
	if first["type"] != "trace_settled" || second["type"] != "run_finished" {
		t.Fatalf("records: %#v %#v", first, second)
	}
}
