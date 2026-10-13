package query

import "fmt"

// View controls selected IDs. Evidence is unaffected by the view.
type View uint8

const (
	// Matches selects only records that directly satisfy selectors.
	Matches View = iota
	// Path selects evidence spans, their ancestors and descendants, and logs
	// attached to those selected spans. It is the default view.
	Path
	// WholeTrace selects every span and log in a matching trace.
	WholeTrace
)

// String returns the view's query-language spelling.
func (v View) String() string {
	if v == Matches {
		return "matches"
	}
	if v == WholeTrace {
		return "trace"
	}
	return "path"
}

// Query is a checked immutable syntax tree. Its zero value is not executable.
type Query struct {
	root expr
	view View
}

// View reports the requested view; an omitted pipeline defaults to Path.
func (q *Query) View() View { return q.view }

// String returns canonical parseable syntax.
func (q *Query) String() string { return Format(q) }

type pos struct{ start, end int }
type expr interface {
	expr()
	span() pos
}
type selector struct {
	p    pos
	kind string
	pred pred
}

func (*selector) expr()       {}
func (s *selector) span() pos { return s.p }

type binary struct {
	p           pos
	op          string
	left, right expr
}

func (*binary) expr()       {}
func (b *binary) span() pos { return b.p }

type pred interface {
	pred()
	span() pos
}
type pbin struct {
	p           pos
	op          string
	left, right pred
}

func (*pbin) pred()       {}
func (p *pbin) span() pos { return p.p }

type pnot struct {
	p pos
	x pred
}

func (*pnot) pred()       {}
func (p *pnot) span() pos { return p.p }

type exists struct {
	p     pos
	field string
}

func (*exists) pred()       {}
func (p *exists) span() pos { return p.p }

type compare struct {
	p         pos
	field, op string
	value     Value
	rawBare   bool
	raw       string
}

func (*compare) pred()       {}
func (p *compare) span() pos { return p.p }

// Error is a syntax or static type diagnostic. Start and End are zero-based
// byte offsets in the original query and delimit the offending source range.
type Error struct {
	Start, End int
	Message    string
}

// Error implements the error interface.
func (e *Error) Error() string { return fmt.Sprintf("query %d:%d: %s", e.Start, e.End, e.Message) }
