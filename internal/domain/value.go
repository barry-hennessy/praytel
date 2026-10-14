// Package domain defines the telemetry records passed between Praytel's ingest,
// correlation, filtering, and rendering stages.
package domain

import (
	"encoding/hex"
	"strconv"
	"strings"
)

// ValueKind identifies the payload stored in a [Value].
type ValueKind uint8

const (
	// ValueInvalid is the zero value and carries no payload.
	ValueInvalid ValueKind = iota
	// ValueString carries a UTF-8 string.
	ValueString
	// ValueInt64 carries a signed 64-bit integer.
	ValueInt64
	// ValueFloat64 carries an IEEE 754 double-precision number.
	ValueFloat64
	// ValueBool carries a Boolean.
	ValueBool
	// ValueBytes carries an arbitrary byte string.
	ValueBytes
	// ValueArray carries an ordered sequence of values.
	ValueArray
	// ValueMap carries an ordered sequence of attributes.
	ValueMap
)

// String returns the stable name used by machine-readable interfaces.
func (k ValueKind) String() string {
	names := [...]string{"invalid", "string", "int64", "float64", "bool", "bytes", "array", "map"}
	if int(k) < 0 || int(k) >= len(names) {
		return "invalid"
	}
	return names[k]
}

// Value is an immutable-by-convention OpenTelemetry value.
type Value struct {
	kind  ValueKind
	s     string
	i     int64
	f     float64
	b     bool
	data  []byte
	list  []Value
	attrs []Attr
}

// StringValue constructs a string value.
func StringValue(v string) Value { return Value{kind: ValueString, s: v} }

// Int64Value constructs an integer value.
func Int64Value(v int64) Value { return Value{kind: ValueInt64, i: v} }

// Float64Value constructs a floating-point value.
func Float64Value(v float64) Value { return Value{kind: ValueFloat64, f: v} }

// BoolValue constructs a Boolean value.
func BoolValue(v bool) Value { return Value{kind: ValueBool, b: v} }

// BytesValue constructs a byte-string value, copying v.
func BytesValue(v []byte) Value { return Value{kind: ValueBytes, data: append([]byte(nil), v...)} }

// ArrayValue constructs an array value, copying v.
func ArrayValue(v []Value) Value { return Value{kind: ValueArray, list: append([]Value(nil), v...)} }

// MapValue constructs an ordered key-value map, copying v.
func MapValue(v []Attr) Value { return Value{kind: ValueMap, attrs: append([]Attr(nil), v...)} }

// Kind reports the value's kind.
func (v Value) Kind() ValueKind { return v.kind }

// AsString returns the string payload and whether the kind matches.
func (v Value) AsString() (string, bool) { return v.s, v.kind == ValueString }

// AsInt64 returns the integer payload and whether the kind matches.
func (v Value) AsInt64() (int64, bool) { return v.i, v.kind == ValueInt64 }

// AsFloat64 returns the floating-point payload and whether the kind matches.
func (v Value) AsFloat64() (float64, bool) { return v.f, v.kind == ValueFloat64 }

// AsBool returns the Boolean payload and whether the kind matches.
func (v Value) AsBool() (bool, bool) { return v.b, v.kind == ValueBool }

// AsBytes returns a copy of the byte payload and whether the kind matches.
func (v Value) AsBytes() ([]byte, bool) { return append([]byte(nil), v.data...), v.kind == ValueBytes }

// AsArray returns a copy of the array payload and whether the kind matches.
func (v Value) AsArray() ([]Value, bool) {
	return append([]Value(nil), v.list...), v.kind == ValueArray
}

// AsMap returns a copy of the ordered map payload and whether the kind matches.
func (v Value) AsMap() ([]Attr, bool) { return append([]Attr(nil), v.attrs...), v.kind == ValueMap }

// String returns a stable, human-readable representation compatible with the
// historical renderer output.
func (v Value) String() string {
	switch v.kind {
	case ValueString:
		return v.s
	case ValueInt64:
		return strconv.FormatInt(v.i, 10)
	case ValueFloat64:
		return strconv.FormatFloat(v.f, 'g', -1, 64)
	case ValueBool:
		return strconv.FormatBool(v.b)
	case ValueBytes:
		return hex.EncodeToString(v.data)
	case ValueArray:
		p := make([]string, len(v.list))
		for i := range v.list {
			p[i] = v.list[i].String()
		}
		return "[" + strings.Join(p, ", ") + "]"
	case ValueMap:
		p := make([]string, len(v.attrs))
		for i, a := range v.attrs {
			p[i] = a.Key + "=" + a.Value.String()
		}
		return "{" + strings.Join(p, ", ") + "}"
	default:
		return ""
	}
}
