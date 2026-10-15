package capture

import (
	"reflect"
	"strings"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/charmbracelet/x/ansi"
)

// A parser recognises one log format. It is a pure function of the line: it
// shares no state with other parsers and knows nothing about them, so parsers
// can run in any order, or at the same time, and [combine] gives the same
// answer.
type parser func(line string) result

// parsers lists every format praytel recognises on stdout and stderr. The
// order carries no meaning.
var parsers = [...]parser{
	parseJSONLine,
	parseTrailingObject,
	parseLogfmtLine,
}

// result is what one parser found in a line.
type result struct {
	fields

	// covered is how many bytes of the line the parser read as its format.
	// Zero means the parser did not recognise the line.
	covered int
	// malformed reports a line that started out as the parser's format but
	// broke part way.
	malformed bool
}

// same reports whether two results carry the same IDs and attributes.
func (r result) same(o result) bool {
	return r.traceID == o.traceID && r.spanID == o.spanID && reflect.DeepEqual(r.attrs, o.attrs)
}

// combine picks one result from every parser's result. It depends only on the
// set of results, never their order. The result that read the most of the line
// wins. If results that read equally much disagree, the line is left unparsed
// rather than guessing between them. A line is malformed only when no parser
// recognised it and at least one saw it break.
func combine(results []result) result {
	var best result
	tied, malformed := false, false
	for _, r := range results {
		malformed = malformed || r.malformed
		switch {
		case r.covered > best.covered:
			best, tied = r, false
		case r.covered > 0 && r.covered == best.covered && !r.same(best):
			tied = true
		}
	}

	if best.covered == 0 {
		return result{malformed: malformed}
	}
	if tied {
		return result{}
	}

	return best
}

type parsed struct {
	domain.LogLine
	malformed bool
}

func parseLine(source domain.Source, body string) parsed {
	// Pretty console writers (zerolog, slog console handlers) colorize
	// key=value tokens with ANSI codes wrapped around them, which both
	// breaks logfmt key matching (e.g. "trace_id" arrives split around
	// escape sequences) and shows up as visible garbage on screen. Strip
	// it once, up front, so both parsing and display work on plain text.
	body = ansi.Strip(body)

	out := parsed{
		LogLine: domain.LogLine{
			Body:   domain.StringValue(body),
			Source: source,
		},
	}

	trim := strings.TrimSpace(body)
	if trim == "" {
		return out
	}

	var results [len(parsers)]result
	for i, p := range parsers {
		results[i] = p(trim)
	}

	r := combine(results[:])
	out.TraceID = r.traceID
	out.SpanID = r.spanID
	out.Attributes = r.attrs
	out.malformed = r.malformed

	return out
}
