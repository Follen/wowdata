package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestMCPCommandHelpExists(t *testing.T) {
	cmd := newRootCommandForRuntime(NewRuntime())
	cmd.SetArgs([]string{"mcp", "stdio", "--help"})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("mcp stdio --help: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "stdio") || !strings.Contains(stdout.String(), "Claude Code") {
		t.Fatalf("help missing stdio client guidance:\n%s", stdout.String())
	}
}

func TestMCPHTTPHelpIncludesClientConfigGuidance(t *testing.T) {
	cmd := newRootCommandForRuntime(NewRuntime())
	cmd.SetArgs([]string{"mcp", "http", "--help"})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("mcp http --help: %v stderr=%s", err, stderr.String())
	}
	help := stdout.String()
	for _, want := range []string{"--host", "--port", "--base-url", "codex mcp add", "cc-switch", "Claude Code"} {
		if !strings.Contains(help, want) {
			t.Fatalf("mcp http help missing %q:\n%s", want, help)
		}
	}
}

func TestMCPWebHelpOmitsServerStartupCommand(t *testing.T) {
	help := mcpHelpHTML("https://mcp.lychee-addon.online:9443")
	if strings.Contains(help, "HTTP server") || strings.Contains(help, "wowdata mcp http") {
		t.Fatalf("web help should not show server startup commands:\n%s", help)
	}
	if !strings.Contains(help, "codex mcp add") || !strings.Contains(help, "claude mcp add --transport http") {
		t.Fatalf("web help should keep client setup commands:\n%s", help)
	}
}

func TestRootHelpIncludesMCPAlias(t *testing.T) {
	cmd := newRootCommandForRuntime(NewRuntime())
	cmd.SetArgs([]string{"--help"})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("--help: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "--mcp") {
		t.Fatalf("root help missing --mcp alias:\n%s", stdout.String())
	}
}

func TestMCPToolsIncludeAllCapabilityGroups(t *testing.T) {
	tools := mcpToolsForRuntime(NewRuntime())
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, want := range []string{
		"wow_warmup",
		"wow_casc",
		"wow_db2",
		"wow_file",
		"wow_icon",
		"wow_spell",
		"wow_encounter",
		"wow_item",
		"wow_creature",
		"wow_decor",
		"wow_video",
	} {
		if !names[want] {
			t.Fatalf("missing MCP tool %s; got %#v", want, names)
		}
	}
	if names["wow_golden"] {
		t.Fatalf("MCP tools should not expose development-only golden helper")
	}
}

func TestMCPToolExecutesCLIHandlerInProcess(t *testing.T) {
	tool := findMCPTool(t, mcpToolsForRuntime(NewRuntime()), "wow_casc")
	result, err := tool.Handler(context.Background(), json.RawMessage(`{"mode":"info"}`))
	if err != nil {
		t.Fatalf("wow_casc tool: %v", err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"ok":false`) || !strings.Contains(string(data), "CASC 未就绪") {
		t.Fatalf("wow_casc should return existing handler envelope, got %s", data)
	}
}

func TestAllBusinessMCPToolsAreCallable(t *testing.T) {
	tests := map[string]string{
		"wow_warmup":    `{}`,
		"wow_casc":      `{"mode":"info"}`,
		"wow_db2":       `{"mode":"schema","table":"SpellName"}`,
		"wow_file":      `{"mode":"lookup","fileDataID":1}`,
		"wow_icon":      `{"fileDataID":1,"output":"output/mcp-test-icon.png"}`,
		"wow_spell":     `{"mode":"info","spellID":1}`,
		"wow_encounter": `{"journalEncounterID":1}`,
		"wow_item":      `{"mode":"get","itemID":1}`,
		"wow_creature":  `{"mode":"display","displayID":1}`,
		"wow_decor":     `{"mode":"list","limit":1}`,
		"wow_video":     `{}`,
	}
	tools := mcpToolsForRuntime(NewRuntime())
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			tool := findMCPTool(t, tools, name)
			result, err := tool.Handler(context.Background(), json.RawMessage(args))
			if err != nil {
				t.Fatalf("%s returned handler error: %v", name, err)
			}
			data, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("marshal result: %v", err)
			}
			if !strings.Contains(string(data), `"ok":`) || !strings.Contains(string(data), `"command":`) {
				t.Fatalf("%s did not return CLI response envelope: %s", name, data)
			}
		})
	}
}

func TestMCPServerListsAndCallsCLIBackedTools(t *testing.T) {
	server := newMCPServerForRuntime(NewRuntime())
	var stdin bytes.Buffer
	writeMCPFrameForTest(&stdin, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	writeMCPFrameForTest(&stdin, []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"wow_casc","arguments":{"mode":"info"}}}`))
	var stdout bytes.Buffer

	if err := server.Serve(context.Background(), &stdin, &stdout); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, `"name":"wow_db2"`) || !strings.Contains(out, `"name":"wow_casc"`) {
		t.Fatalf("tools/list missing CLI-backed tools:\n%s", out)
	}
	if !strings.Contains(out, `CASC 未就绪`) {
		t.Fatalf("tools/call did not execute CLI-backed handler:\n%s", out)
	}
}

func findMCPTool(t *testing.T, tools []mcpTool, name string) mcpTool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %s not found", name)
	return mcpTool{}
}

func writeMCPFrameForTest(out io.Writer, payload []byte) {
	fmt.Fprintf(out, "Content-Length: %d\r\n\r\n", len(payload))
	out.Write(payload)
}
