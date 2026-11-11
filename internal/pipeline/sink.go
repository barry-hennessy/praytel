package pipeline

import (
	"fmt"
	"io"
	"os"

	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/view"
)

// Sink is the destination of an output.
type Sink interface {
	// Write sends lines. Each line gets a newline.
	Write(lines []string) error
	// Close ends the output and returns the exit code it asks for, given
	// the child's.
	Close(childCode int) (code int)
}

// writerSink writes each line as it comes.
type writerSink struct {
	w      io.Writer
	closer io.Closer
}

func (s writerSink) Write(lines []string) error {
	for _, l := range lines {
		if _, err := io.WriteString(s.w, l+"\n"); err != nil {
			return err
		}
	}
	return nil
}

func (s writerSink) Close(int) int {
	if s.closer != nil {
		_ = s.closer.Close()
	}
	return 0
}

// exitSink holds its output until the child's exit code is known, then
// writes it only if the view reports it complete:
//
//   - Child failed: nothing is written, the child's captured output is
//     replayed to stderr, and the child's code wins.
//   - Child succeeded, output incomplete: nothing is written, the problems
//     go to stderr, and the code is plan.IncompleteExit.
//   - Otherwise the output is written.
type exitSink struct {
	inner  Sink
	name   string
	report view.Reporter
	stderr io.Writer
	held   []string
}

func (s *exitSink) Write(lines []string) error {
	s.held = append(s.held, lines...)
	return nil
}

func (s *exitSink) Close(childCode int) int {
	defer s.inner.Close(childCode)
	if childCode != 0 {
		for _, l := range s.report.Captured() {
			_, _ = fmt.Fprintln(s.stderr, l.Body)
		}
		s.problems()
		return childCode
	}
	if len(s.report.Problems()) > 0 {
		s.problems()
		return plan.IncompleteExit
	}
	_ = s.inner.Write(s.held)
	return 0
}

func (s *exitSink) problems() {
	for _, p := range s.report.Problems() {
		_, _ = fmt.Fprintf(s.stderr, "praytel: %s incomplete: %s\n", s.name, p)
	}
}

func buildSink(o plan.Output, v view.View, deps Deps) (Sink, error) {
	var inner Sink
	switch o.Sink.Kind {
	case plan.SinkStdout:
		inner = writerSink{w: deps.Stdout}
	case plan.SinkStderr:
		inner = writerSink{w: deps.Stderr}
	case plan.SinkFile:
		if o.Sink.Templated() {
			return fileTemplateSink{pattern: o.Sink.Path}, nil
		}
		f, err := os.Create(o.Sink.Path)
		if err != nil {
			return nil, fmt.Errorf("sink file: %w", err)
		}
		inner = writerSink{w: f, closer: f}
	default:
		return nil, fmt.Errorf("sink %q is not run by the pipeline", o.Sink.Kind)
	}
	if !o.Sink.Exit {
		return inner, nil
	}
	if v, ok := v.(view.Verdict); ok {
		return &verdictSink{inner: inner, verdict: v}, nil
	}
	rep, ok := v.(view.Reporter)
	if !ok {
		_ = inner.Close(0)
		return nil, fmt.Errorf("view %q cannot decide the exit code", o.View.Kind)
	}
	name := o.Name
	if name == "" {
		name = o.View.Kind
	}
	return &exitSink{inner: inner, name: name, report: rep, stderr: deps.Stderr}, nil
}

// verdictSink holds its output until the end, then writes it and returns the
// exit code its view decided; a failed child's code still wins.
type verdictSink struct {
	inner   Sink
	verdict view.Verdict
	held    []string
}

func (s *verdictSink) Write(lines []string) error {
	s.held = append(s.held, lines...)
	return nil
}

func (s *verdictSink) Close(childCode int) int {
	defer s.inner.Close(childCode)
	_ = s.inner.Write(s.held)
	if childCode != 0 {
		return childCode
	}
	return s.verdict.ExitCode()
}
