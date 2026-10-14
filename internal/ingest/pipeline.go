// Package ingest wires a correlator to a listening OTLP receiver: the
// setup every composition mode (direct CLI run, in-process agent session)
// otherwise repeats. See docs/guides/architecture.md's Ingest and Join sections.
package ingest

import (
	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/correlator"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/receiver"
)

// Pipeline is a correlator bound to its OTLP receiver, ready to Run/Serve.
type Pipeline struct {
	Events chan domain.Event
	Corr   *correlator.Correlator
	Rec    *receiver.Receiver
}

// New starts an OTLP receiver, creates a correlator fed by it, and wires the
// receiver to drop ingest once the correlator stops accepting it.
func New(lt observability.LoggingTracer, drain correlator.Drain, limits correlator.Limits, ports receiver.Ports) (Pipeline, error) {
	events := make(chan domain.Event, 64)
	corr := correlator.New(lt, events, drain, limits)
	rec, err := receiver.Listen(lt, ports, corr.Spans(), corr.Logs())
	if err != nil {
		return Pipeline{}, err
	}
	corr.DropIngest(rec.Drop)
	return Pipeline{Events: events, Corr: corr, Rec: rec}, nil
}
