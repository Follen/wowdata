package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
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

func TestMCPHTTPHelpUsesGenericExampleDomain(t *testing.T) {
	cmd := newRootCommandForRuntime(NewRuntime())
	cmd.SetArgs([]string{"mcp", "http", "--help"})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("mcp http --help: %v stderr=%s", err, stderr.String())
	}
	help := stdout.String()
	if strings.Contains(help, "lychee-addon.online") || strings.Contains(help, "wowdata.online") {
		t.Fatalf("Go HTTP help should use generic examples, not deployment domains:\n%s", help)
	}
	if !strings.Contains(help, "https://mcp.example.com:9443/mcp") {
		t.Fatalf("Go HTTP help should include generic MCP endpoint example:\n%s", help)
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

func TestMCPHTTPWarmupGateReturnsBusyEnvelope(t *testing.T) {
	rt := NewRuntime()
	rt.enableHTTPWarmupGate()
	release, err := rt.beginHTTPWarmup()
	if err != nil {
		t.Fatalf("begin warmup: %v", err)
	}
	defer release()

	tool := findMCPTool(t, mcpToolsForRuntime(rt), "wow_warmup")
	result, err := tool.Handler(context.Background(), json.RawMessage(`{"source":"remote","region":"cn","product":"wow"}`))
	if err != nil {
		t.Fatalf("wow_warmup tool: %v", err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"ok":false`) || !strings.Contains(string(data), "warmup_in_progress") {
		t.Fatalf("busy warmup envelope mismatch: %s", data)
	}
}

func TestMCPArtifactConfigAddsDownloadURLForExportResults(t *testing.T) {
	root := t.TempDir()
	artifactPath := filepath.Join(root, "icons", "134400.png")
	result := map[string]interface{}{
		"ok":      true,
		"command": "icon export",
		"data": map[string]interface{}{
			"path":     artifactPath,
			"uri":      "file://" + filepath.ToSlash(artifactPath),
			"mimeType": "image/png",
			"size":     42,
		},
	}

	augmented := addArtifactDownloadLinks(result, artifactConfig{
		root:    root,
		baseURL: "https://mcp.example.com/files",
	}).(map[string]interface{})
	data := augmented["data"].(map[string]interface{})

	if data["downloadUrl"] != "https://mcp.example.com/files/icons/134400.png" {
		t.Fatalf("downloadUrl = %#v", data["downloadUrl"])
	}
	if data["uri"] != "https://mcp.example.com/files/icons/134400.png" {
		t.Fatalf("uri should prefer public artifact URL, got %#v", data["uri"])
	}
	if data["fileURI"] == "" {
		t.Fatalf("fileURI should preserve original local file URI: %#v", data)
	}
}

func TestMCPArtifactConfigIgnoresPathsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	result := map[string]interface{}{
		"ok": true,
		"data": map[string]interface{}{
			"path": filepath.Join(other, "secret.png"),
			"uri":  "file://" + filepath.ToSlash(filepath.Join(other, "secret.png")),
		},
	}

	augmented := addArtifactDownloadLinks(result, artifactConfig{
		root:    root,
		baseURL: "https://mcp.example.com/files",
	}).(map[string]interface{})
	data := augmented["data"].(map[string]interface{})

	if _, ok := data["downloadUrl"]; ok {
		t.Fatalf("downloadUrl should not be added for paths outside artifact root: %#v", data)
	}
}

func TestMCPArtifactConfigDefaultsIconOutput(t *testing.T) {
	root := t.TempDir()
	args := map[string]interface{}{
		"fileDataID": float64(134400),
	}

	out, err := iconArgsWithArtifacts(args, artifactConfig{root: root})
	if err != nil {
		t.Fatalf("iconArgsWithArtifacts: %v", err)
	}

	want := filepath.Join(root, "icons", "134400.png")
	if !stringSliceContainsSequence(out, "--output", want) {
		t.Fatalf("expected default output %q in %#v", want, out)
	}
}

func TestMCPArtifactConfigDefaultsFileExportOutput(t *testing.T) {
	root := t.TempDir()
	args := map[string]interface{}{
		"mode":       "export",
		"fileDataID": float64(456),
	}

	out, err := fileArgsWithArtifacts(args, artifactConfig{root: root})
	if err != nil {
		t.Fatalf("fileArgsWithArtifacts: %v", err)
	}

	want := filepath.Join(root, "files", "456.bin")
	if !stringSliceContainsSequence(out, "--output", want) {
		t.Fatalf("expected default output %q in %#v", want, out)
	}
}

func TestMCPArtifactConfigDefaultsFileExportOutputFromFilename(t *testing.T) {
	root := t.TempDir()
	args := map[string]interface{}{
		"mode":     "export",
		"filename": "interface/icons/inv_misc_questionmark.blp",
	}

	out, err := fileArgsWithArtifacts(args, artifactConfig{root: root})
	if err != nil {
		t.Fatalf("fileArgsWithArtifacts: %v", err)
	}

	want := filepath.Join(root, "files", "inv_misc_questionmark.blp")
	if !stringSliceContainsSequence(out, "--output", want) {
		t.Fatalf("expected default output %q in %#v", want, out)
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

func stringSliceContainsSequence(values []string, first, second string) bool {
	for i := 0; i < len(values)-1; i++ {
		if values[i] == first && values[i+1] == second {
			return true
		}
	}
	return false
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
