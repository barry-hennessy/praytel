// Package query renders query matches as deterministic text, JSON, or NDJSON.
//
// Renderer consumes the same value-copy event stream as the interactive and
// append-only renderers. It owns an event-derived [projection.Projection], so it
// never reads correlator state directly.
package query
