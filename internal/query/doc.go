// Package query implements Praytel's pure, typed trace query language.
//
// Queries select spans and logs, combine selectors within a trace, and relate
// records through parent/descendant operators. Predicates inside one selector
// must match the same record; && and || between selectors combine evidence from
// the same trace. The > operator means direct child and >> means descendant.
//
// Comparisons preserve telemetry types and never coerce. A missing field does
// not satisfy !=; use !exists(field) to select absence. The *= operator performs
// literal substring matching. =~ and !~ use fully anchored Go regular
// expressions. Duration literals, such as 100ms, compare with duration
// intrinsics represented as nanoseconds.
//
// Intrinsics include span:name, span:kind, span:status, span:duration,
// trace:id, trace:duration, log:body, log:source, and log:severity_number.
// Attribute fields retain their OpenTelemetry keys and may be qualified with
// resource., scope., span., or log. Parse and Check perform no I/O; Query.Eval
// operates on an immutable Trace value supplied by the caller.
//
// A typical query is:
//
//	log{ log:body *= "retry" && log:severity_number >= 17 } && span{ span:kind = client } | path
package query
