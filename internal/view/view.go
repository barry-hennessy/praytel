// Package view holds the contract between the pipeline driver and the
// views it runs. It is a leaf: views implement it without importing the
// driver. See design/PIPELINE.md.
package view

import (
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/projection"
)

// Viewport is the visible part of a TUI pane. The zero value means "all of
// it": a view makes every line.
type Viewport struct {
	Width, Height int
	Top           int
}

// View makes output from the projection.
type View interface {
	// Observe sees each event after the output's stages, with the output's
	// reader of the projection, which already holds the event.
	Observe(ev domain.Event, r projection.Reader)
	// Render makes the lines to send now. The driver calls it when the
	// output's trigger fires.
	Render(r projection.Reader, vp Viewport) []string
}

// Captured is one line the child wrote to stdout or stderr.
type Captured struct {
	Source domain.Source
	Body   string
}

// Reporter is a view that can say why its output is not complete, and that
// keeps the child's captured output for an exit sink to replay.
type Reporter interface {
	Problems() []string
	Captured() []Captured
}

// Interrupter is a view that must know when the run was cut short before
// its events ended.
type Interrupter interface {
	Interrupt()
}

// Finisher is a view with something to say once the run is final, after
// its last per-trace render: a report over the whole run.
type Finisher interface {
	Finish(r projection.Reader) []string
}

// Verdict is a view that decides praytel's exit code itself, such as a
// check: 0 to pass, or a code for what went wrong. An exit sink writes the
// view's lines whatever the code.
type Verdict interface {
	ExitCode() int
}
