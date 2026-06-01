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
