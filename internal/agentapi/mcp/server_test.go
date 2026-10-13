package mcp_test

import (
	"context"
	"sort"
	"testing"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/agentapi"
	agentmcp "github.com/barry-hennessy/praytel/internal/agentapi/mcp"
	"github.com/barry-hennessy/praytel/internal/session"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolsAndProgressiveRun(t *testing.T) {
	manager, err := session.NewManager(t.TempDir(), observability.LoggingTracer{}, session.ManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	server := agentmcp.NewServer(agentapi.New(manager), "test")
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	listed, err := clientSession.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(listed.Tools))
	for i, tool := range listed.Tools {
		names[i] = tool.Name
		if tool.InputSchema == nil || tool.OutputSchema == nil {
			t.Fatalf("tool %s lacks inferred schemas", tool.Name)
		}
	}
	sort.Strings(names)
	want := []string{"get_logs", "get_span_details", "get_trace_topology", "start_run", "stop_run", "wait_for_matches"}
	if len(names) != len(want) {
		t.Fatalf("tools=%v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("tools=%v", names)
		}
	}

	start, err := clientSession.CallTool(t.Context(), &sdk.CallToolParams{Name: "start_run", Arguments: map[string]any{
		"command": []any{"sh", "-c", "printf 'needle log\\n'"},
		"query":   `log{ log:body *= "needle" } | matches`,
	}})
	if err != nil || start.IsError {
		t.Fatalf("start: result=%#v err=%v", start, err)
	}
	startMap := start.StructuredContent.(map[string]any)
	runID := startMap["run_id"].(string)

	var waitMap map[string]any
	for range 10 {
		wait, callErr := clientSession.CallTool(context.Background(), &sdk.CallToolParams{Name: "wait_for_matches", Arguments: map[string]any{
			"run_id": runID, "cursor": float64(0), "timeout_ms": float64(1000), "limit": float64(10),
		}})
		if callErr != nil || wait.IsError {
			t.Fatalf("wait: result=%#v err=%v", wait, callErr)
		}
		waitMap = wait.StructuredContent.(map[string]any)
		if waitMap["run_state"] == "exited" {
			break
		}
	}
	if waitMap["run_state"] != "exited" || waitMap["matched"].(float64) < 1 || waitMap["truncated"] != false {
		t.Fatalf("wait=%#v", waitMap)
	}
	if _, ok := waitMap["environment"]; ok {
		t.Fatal("environment leaked into response")
	}
}
