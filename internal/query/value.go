package query

import (
	"bytes"
	"fmt"
	"strconv"
)

// ValueKind identifies the exact representation of a Value. Values of
// different kinds never compare equal and are never coerced.
type ValueKind uint8

const (
	// InvalidKind is the zero kind and carries no value.
	InvalidKind ValueKind = iota
	// StringKind identifies strings.
	StringKind
	// IntKind identifies signed 64-bit integers and durations in nanoseconds.
	IntKind
	// FloatKind identifies IEEE 754 double-precision values.
	FloatKind
	// BoolKind identifies Booleans.
	BoolKind
	// BytesKind identifies byte strings.
	BytesKind
	// ArrayKind identifies ordered value sequences.
	ArrayKind
	// MapKind identifies key-value collections.
	MapKind
)

// String returns the stable kind name used in diagnostics.
func (k ValueKind) String() string {
	names := [...]string{"invalid", "string", "int64", "float64", "bool", "bytes", "array", "map"}
	if int(k) >= len(names) {
		return "invalid"
	}
	return names[k]
}

// Value is a typed telemetry value. Constructors preserve the dynamic type;
// ArrayValue and MapValue defensively copy their inputs.
type Value struct {
	k  ValueKind
	s  string
	i  int64
	f  float64
	b  bool
	by []byte
	a  []Value
	m  map[string]Value
}

// StringValue constructs a string Value.
func StringValue(v string) Value { return Value{k: StringKind, s: v} }

// IntValue constructs an int64 Value.
func IntValue(v int64) Value { return Value{k: IntKind, i: v} }

// FloatValue constructs a float64 Value.
func FloatValue(v float64) Value { return Value{k: FloatKind, f: v} }

// BoolValue constructs a boolean Value.
func BoolValue(v bool) Value { return Value{k: BoolKind, b: v} }

// BytesValue constructs a Value with a defensive copy.
func BytesValue(v []byte) Value { return Value{k: BytesKind, by: append([]byte(nil), v...)} }

// ArrayValue constructs a Value with a defensive slice copy.
func ArrayValue(v []Value) Value { return Value{k: ArrayKind, a: append([]Value(nil), v...)} }

// MapValue constructs a Value with a defensive map copy.
func MapValue(v map[string]Value) Value {
	c := make(map[string]Value, len(v))
	for k, x := range v {
		c[k] = x
	}
	return Value{k: MapKind, m: c}
}

// Kind reports the exact dynamic type.
func (v Value) Kind() ValueKind { return v.k }

// String formats the value as a query literal where possible.
func (v Value) String() string {
	switch v.k {
	case StringKind:
		return strconv.Quote(v.s)
	case IntKind:
		return strconv.FormatInt(v.i, 10)
	case FloatKind:
		return strconv.FormatFloat(v.f, 'g', -1, 64)
	case BoolKind:
		return strconv.FormatBool(v.b)
	case BytesKind:
		return fmt.Sprintf("%q", v.by)
	default:
		return fmt.Sprint(v.Interface())
	}
}

// Interface returns the underlying value, defensively copying containers.
func (v Value) Interface() any {
	switch v.k {
	case StringKind:
		return v.s
	case IntKind:
		return v.i
	case FloatKind:
		return v.f
	case BoolKind:
		return v.b
	case BytesKind:
		return append([]byte(nil), v.by...)
	case ArrayKind:
		return append([]Value(nil), v.a...)
	case MapKind:
		c := map[string]Value{}
		for k, x := range v.m {
			c[k] = x
		}
		return c
	}
	return nil
}
func equal(a, b Value) bool {
	if a.k != b.k {
		return false
	}
	switch a.k {
	case StringKind:
		return a.s == b.s
	case IntKind:
		return a.i == b.i
	case FloatKind:
		return a.f == b.f
	case BoolKind:
		return a.b == b.b
	case BytesKind:
		return bytes.Equal(a.by, b.by)
	}
	return false
}

// Attr is one telemetry attribute. Ordered slices preserve source order;
// lookup uses the first occurrence of an exact key.
type Attr struct {
	Key   string
	Value Value
}

// Scope contains instrumentation scope metadata and attributes.
type Scope struct {
	Name, Version string
	Attributes    []Attr
}

// Resource contains resource attributes.
type Resource struct{ Attributes []Attr }

// Span is an immutable-by-convention span snapshot. ParentID is empty for a
// root. Attributes should not be mutated while Eval is running.
type Span struct {
	ID, ParentID, Name, Kind, Status, StatusMessage string
	Duration                                        int64
	Resource                                        Resource
	Scope                                           Scope
	Attributes                                      []Attr
}

// Log is an immutable-by-convention log snapshot. SpanID is the final assigned
// span; an empty SpanID denotes a pending/unassigned log.
type Log struct {
	ID, SpanID, Body, Source, SeverityText string
	SeverityNumber                         int64
	Resource                               Resource
	Scope                                  Scope
	Attributes                             []Attr
}

// Trace is the package-local evaluation snapshot. TraceID and Duration are
// available as trace:id and trace:duration. Eval never mutates it.
type Trace struct {
	ID       string
	Duration int64
	Spans    []Span
	Logs     []Log
}
