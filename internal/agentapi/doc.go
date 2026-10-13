// Package agentapi exposes Praytel sessions through transport-neutral,
// JSON-shaped operations.
//
// The package deliberately contains no MCP types. Protocol adapters register
// these operations and retain one source of truth for validation, limits, and
// completeness metadata.
package agentapi
