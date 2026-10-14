// Package projection maintains a concurrent, event-derived view of one telemetry
// run. It is the boundary between mutable correlation and immutable query/MCP
// snapshots; callers replay domain events and never inspect correlator state.
package projection
