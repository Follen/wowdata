package main

import (
	"os"

	"github.com/spf13/cobra"
)

func newMCPStdioCommand(rt *Runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "stdio",
		Short: "Serve MCP tools over stdio.",
		Long: `Serve MCP tools over stdio.

Use this for local clients that launch wowdata as a subprocess.

Codex CLI:
  codex mcp add wowdata -- wowdata mcp stdio

Claude Code:
  claude mcp add wowdata -- wowdata mcp stdio

cc-switch custom MCP:
  {
    "type": "stdio",
    "command": "wowdata",
    "args": ["mcp", "stdio"]
  }

Legacy compatibility:
  wowdata --mcp is treated as wowdata mcp stdio.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			server := newMCPServerForRuntime(rt)
			return server.Serve(cmd.Context(), os.Stdin, os.Stdout)
		},
	}
}
