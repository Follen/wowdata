package main

import "testing"

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
