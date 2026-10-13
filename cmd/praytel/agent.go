package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/agentapi"
	agentmcp "github.com/barry-hennessy/praytel/internal/agentapi/mcp"
	"github.com/barry-hennessy/praytel/internal/session"
)

func runMCP(ctx context.Context, lt observability.LoggingTracer) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("working directory: %w", err)
	}
	manager, err := session.NewManager(root, lt, session.ManagerOptions{})
	if err != nil {
		return err
	}
	defer manager.Close()
	return agentmcp.RunStdio(ctx, agentmcp.NewServer(agentapi.New(manager), buildVersion()))
}

func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func runAgentCommand(args []string, stdout io.Writer) error {
	if len(args) != 1 || args[0] != "instructions" {
		return fmt.Errorf("usage: praytel agent instructions")
	}
	_, err := fmt.Fprintln(stdout, agentapi.Instructions)
	return err
}
