// Package mcp adapts [agentapi.Service] to the Model Context Protocol.
//
// It is intentionally a thin schema and stdio transport boundary. Session
// lifecycle, query semantics, limits, and completeness rules remain in the
// transport-neutral packages beneath it.
package mcp
