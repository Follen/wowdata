package main

import (
	"bytes"
	"context"
	"testing"

	"wowdata/internal/config"
	httpservice "wowdata/internal/service/http"
)

func TestStdioMCPToolNamesStayStable(t *testing.T) {
	rt := NewRuntime()
	server := newMCPServerForRuntime(rt)
	names := map[string]bool{}
	for _, name := range server.ToolNamesForTest() {
		names[name] = true
	}
	want := []string{
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
	}
	if len(names) != len(want) {
		t.Fatalf("tool count = %d, want %d: %#v", len(names), len(want), names)
	}
	for _, name := range want {
		if !names[name] {
			t.Fatalf("tool %q missing; all=%#v", name, names)
		}
	}
}

func TestMCPCommandHasStdioAndHTTPSubcommands(t *testing.T) {
	rt := NewRuntime()
	root := newRootCommandForRuntime(rt)
	mcp, _, err := root.Find([]string{"mcp"})
	if err != nil {
		t.Fatalf("find mcp: %v", err)
	}
	for _, name := range []string{"stdio", "http"} {
		if child, _, err := mcp.Find([]string{name}); err != nil || child.Name() != name {
			t.Fatalf("mcp subcommand %q missing: child=%v err=%v", name, child, err)
		}
	}
}

func TestHTTPMCPToolNamesExcludeWarmupAndIncludeStatusBuilds(t *testing.T) {
	svc := httpservice.NewService(config.DefaultHTTPConfig(), nil)
	server := newMCPHTTPServerForService(svc, artifactConfig{})
	var stdin, stdout bytes.Buffer
	writeMCPFrameForTest(&stdin, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))

	if err := server.Serve(context.Background(), &stdin, &stdout); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	out := stdout.String()
	if bytes.Contains(stdout.Bytes(), []byte(`"name":"wow_warmup"`)) {
		t.Fatalf("HTTP MCP tools/list should not include wow_warmup:\n%s", out)
	}
	for _, want := range []string{`"name":"wow_status"`, `"name":"wow_builds"`} {
		if !bytes.Contains(stdout.Bytes(), []byte(want)) {
			t.Fatalf("HTTP MCP tools/list missing %s:\n%s", want, out)
		}
	}
}
