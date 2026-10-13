// Package telemetry converts domain telemetry into the JSON-safe wire types
// agentapi returns to MCP clients.
package telemetry

import (
	"encoding/hex"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
)

// Value is a JSON-safe typed telemetry value.
type Value struct {
	Kind    string `json:"kind"`
	Display string `json:"display"`
	Data    any    `json:"data,omitempty"`
}

// Attribute is an ordered typed telemetry attribute.
type Attribute struct {
	Key   string `json:"key"`
	Value Value  `json:"value"`
}

// Scope identifies the instrumentation library that emitted a record.
type Scope struct {
	Name       string      `json:"name,omitempty"`
	Version    string      `json:"version,omitempty"`
	Attributes []Attribute `json:"attributes,omitempty"`
}

// SpanEvent is one timestamped event recorded by a span.
type SpanEvent struct {
	Name                   string      `json:"name"`
	Timestamp              time.Time   `json:"timestamp"`
	Attributes             []Attribute `json:"attributes,omitempty"`
	DroppedAttributesCount uint32      `json:"dropped_attributes_count,omitempty"`
}

// SpanLink identifies another span context related to a span.
type SpanLink struct {
	TraceID                string      `json:"trace_id"`
	SpanID                 string      `json:"span_id"`
	TraceState             string      `json:"trace_state,omitempty"`
	Attributes             []Attribute `json:"attributes,omitempty"`
	DroppedAttributesCount uint32      `json:"dropped_attributes_count,omitempty"`
}

// ToAttributes converts ordered domain attributes to wire attributes.
func ToAttributes(in []domain.Attr) []Attribute {
	out := make([]Attribute, len(in))
	for i, attr := range in {
		out[i] = Attribute{attr.Key, ToValue(attr.Value)}
	}
	return out
}

// ToScope converts a domain instrumentation scope to its wire form.
func ToScope(in domain.Scope) Scope {
	return Scope{Name: in.Name, Version: in.Version, Attributes: ToAttributes(in.Attributes)}
}

// ToEvents converts domain span events to their wire form.
func ToEvents(in []domain.SpanEvent) []SpanEvent {
	out := make([]SpanEvent, len(in))
	for i, event := range in {
		out[i] = SpanEvent{event.Name, event.Timestamp, ToAttributes(event.Attributes), event.DroppedAttributesCount}
	}
	return out
}

// ToLinks converts domain span links to their wire form.
func ToLinks(in []domain.SpanLink) []SpanLink {
	out := make([]SpanLink, len(in))
	for i, link := range in {
		out[i] = SpanLink{link.TraceID, link.SpanID, link.TraceState, ToAttributes(link.Attributes), link.DroppedAttributesCount}
	}
	return out
}

// ToValue converts a domain value to its JSON-safe wire form, preserving
// telemetry type via Kind/Display and exposing a native Data payload where
// JSON can represent one directly.
func ToValue(in domain.Value) Value {
	out := Value{Kind: in.Kind().String(), Display: in.String()}
	switch in.Kind() {
	case domain.ValueString:
		out.Data, _ = in.AsString()
	case domain.ValueInt64:
		out.Data, _ = in.AsInt64()
	case domain.ValueFloat64:
		out.Data, _ = in.AsFloat64()
	case domain.ValueBool:
		out.Data, _ = in.AsBool()
	case domain.ValueBytes:
		b, _ := in.AsBytes()
		out.Data = hex.EncodeToString(b)
	case domain.ValueArray:
		items, _ := in.AsArray()
		values := make([]any, len(items))
		for i, item := range items {
			values[i] = ToValue(item)
		}
		out.Data = values
	case domain.ValueMap:
		items, _ := in.AsMap()
		out.Data = ToAttributes(items)
	}
	return out
}
