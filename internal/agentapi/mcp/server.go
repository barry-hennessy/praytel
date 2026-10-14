package mcp

import (
	"context"

	"github.com/barry-hennessy/praytel/internal/agentapi"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var falseValue = false
var trueValue = true

// NewServer constructs a Praytel MCP server and registers its progressive
// trace/log investigation tools.
func NewServer(api *agentapi.Service, version string) *sdk.Server {
	server := sdk.NewServer(&sdk.Implementation{Name: "praytel", Version: version}, nil)
	readOnly := func(title string) *sdk.ToolAnnotations {
		return &sdk.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &falseValue, DestructiveHint: &falseValue}
	}
	sdk.AddTool(server, &sdk.Tool{
		Name: "start_run", Description: "Start a local command under Praytel and collect joined traces and logs. This executes a process and is not idempotent. Use wait_for_matches with the returned run_id and cursor.",
		Annotations: &sdk.ToolAnnotations{Title: "Start Praytel run", OpenWorldHint: &falseValue, DestructiveHint: &falseValue},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in agentapi.StartInput) (*sdk.CallToolResult, agentapi.StartOutput, error) {
		out, err := api.Start(ctx, in)
		return nil, out, err
	})
	sdk.AddTool(server, &sdk.Tool{
		Name: "wait_for_matches", Description: "Wait from an idempotent cursor for compact matching trace revisions. A timeout is returned as timed_out=true. Do not claim absence unless run_state is terminal, truncated=false, and incomplete_reasons is empty.",
		Annotations: readOnly("Wait for Praytel matches"),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in agentapi.WaitInput) (*sdk.CallToolResult, agentapi.WaitOutput, error) {
		out, err := api.Wait(ctx, in)
		return nil, out, err
	})
	sdk.AddTool(server, &sdk.Tool{
		Name: "get_trace_topology", Description: "Return a bounded depth-first trace skeleton without attributes or log bodies. Use this before requesting rich details.",
		Annotations: readOnly("Get trace topology"),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in agentapi.TopologyInput) (*sdk.CallToolResult, agentapi.TopologyOutput, error) {
		out, err := api.Topology(ctx, in)
		return nil, out, err
	})
	sdk.AddTool(server, &sdk.Tool{
		Name: "get_span_details", Description: "Return typed details for at most 20 explicit suspicious span IDs from topology or query evidence.",
		Annotations: readOnly("Get span details"),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in agentapi.DetailsInput) (*sdk.CallToolResult, agentapi.DetailsOutput, error) {
		out, err := api.Details(ctx, in)
		return nil, out, err
	})
	sdk.AddTool(server, &sdk.Tool{
		Name: "get_logs", Description: "Return typed details for at most 50 explicit joined log IDs from query evidence.",
		Annotations: readOnly("Get joined logs"),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in agentapi.LogsInput) (*sdk.CallToolResult, agentapi.LogsOutput, error) {
		out, err := api.Logs(ctx, in)
		return nil, out, err
	})
	sdk.AddTool(server, &sdk.Tool{
		Name: "stop_run", Description: "Send SIGTERM to a run's child process and wait for telemetry drain. This changes process state and is not idempotent across unknown run IDs.",
		Annotations: &sdk.ToolAnnotations{Title: "Stop Praytel run", OpenWorldHint: &falseValue, DestructiveHint: &trueValue},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in agentapi.StopInput) (*sdk.CallToolResult, agentapi.StopOutput, error) {
		out, err := api.Stop(ctx, in)
		return nil, out, err
	})
	return server
}

// RunStdio serves server on stdin/stdout until ctx ends or the client closes.
func RunStdio(ctx context.Context, server *sdk.Server) error {
	return server.Run(ctx, &sdk.StdioTransport{})
}
