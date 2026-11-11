// Package query is praytel's query language over one trace at a time. A
// query selects spans and logs (span{…}, log{…}) and combines the selections
// within a trace: && (the trace has both), || (either), & (items in both),
// ! (items not in), > (direct child) and >> (descendant). See
// design/IDEAS.md, "A query language for logs and traces", and
// design/PIPELINE.md, "Selections".
//
// The package parses, formats, checks and evaluates queries. Parse and the
// builders make the same tree, and Format prints the canonical text that
// parses back to it.
package query

import "regexp"

// Kind is the kind of record a selector picks.
type Kind uint8

const (
	// KindSpan selects spans.
	KindSpan Kind = iota
	// KindLog selects logs.
	KindLog
)

func (k Kind) String() string {
	if k == KindLog {
		return "log"
	}
	return "span"
}

// Expr is a query: a set of spans and logs within one trace.
type Expr interface{ expr() }

type (
	// Select is span{Pred} or log{Pred}: every record of Kind that Pred
	// matches. A nil Pred matches every record.
	Select struct {
		Kind Kind
		Pred Pred
	}
	// Both is L && R: if the trace has items of both, the items of both;
	// otherwise none. The two need not be the same span.
	Both struct{ L, R Expr }
	// Either is L || R: the items of either.
	Either struct{ L, R Expr }
	// Inter is L & R: the items in both.
	Inter struct{ L, R Expr }
	// Not is !X: the spans of the trace not in X.
	Not struct{ X Expr }
	// Child is Parent > Kid: the items of Kid whose span's parent is in
	// Parent.
	Child struct{ Parent, Kid Expr }
	// Desc is Anc >> Kid: the items of Kid whose span has an ancestor in
	// Anc.
	Desc struct{ Anc, Kid Expr }
)

func (Select) expr() {}
func (Both) expr()   {}
func (Either) expr() {}
func (Inter) expr()  {}
func (Not) expr()    {}
func (Child) expr()  {}
func (Desc) expr()   {}

// Pred is a condition on one record.
type Pred interface{ pred() }

type (
	// Cmp is Field Op Value.
	Cmp struct {
		Field Field
		Op    Op
		Value Value
		re    *regexp.Regexp // for =~ and !~, set by Check
	}
	// Exists is exists(Field): the record has the field.
	Exists struct{ Field Field }
	// PNot is !P.
	PNot struct{ P Pred }
	// PAnd is L && R.
	PAnd struct{ L, R Pred }
	// POr is L || R.
	POr struct{ L, R Pred }
)

func (*Cmp) pred()   {}
func (Exists) pred() {}
func (PNot) pred()   {}
func (PAnd) pred()   {}
func (POr) pred()    {}

// Op is a comparison.
type Op uint8

// Comparisons. =~ and !~ are anchored RE2 regular expressions; *= is a
// literal substring.
const (
	Eq Op = iota
	Ne
	Gt
	Ge
	Lt
	Le
	Re
	NRe
	Contains
)

var opText = [...]string{"=", "!=", ">", ">=", "<", "<=", "=~", "!~", "*="}

func (o Op) String() string { return opText[o] }

// Field names what a predicate reads.
//
//   - Intrinsic fields: span:name, span:status, span:kind, span:component,
//     span:id, span:parent_id, trace:id, log:body, log:source,
//     log:severity, log:severity_text.
//   - Attributes: span.KEY, log.KEY (of the record), resource.KEY (of the
//     span's resource). A bare KEY in a selector is an attribute of its
//     record.
type Field struct {
	// Scope is "span", "log", "trace" or "resource".
	Scope string
	// Intrinsic is true for scope:name, false for scope.KEY.
	Intrinsic bool
	Name      string
}

// ValueKind is the type of a literal.
type ValueKind uint8

const (
	// String is a quoted string.
	String ValueKind = iota
	// Int is an integer.
	Int
	// Float is a number with a fraction or exponent.
	Float
	// Bool is true or false.
	Bool
	// Ident is a bare word, for the enumerations: span:status (unset, ok,
	// error), span:kind (unspecified, internal, server, client, producer,
	// consumer) and log:source (stdout, stderr, otlp).
	Ident
)

// Value is a literal.
type Value struct {
	Kind ValueKind
	S    string
	I    int64
	F    float64
	B    bool
}
