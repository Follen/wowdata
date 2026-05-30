package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResponseEnvelopeSuccess(t *testing.T) {
	resp := NewSuccessResponse("wowdata test", map[string]any{"value": 42})

	if !resp.OK {
		t.Fatalf("expected OK response")
	}
	if resp.Command != "wowdata test" {
		t.Fatalf("command = %q", resp.Command)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %#v", resp.Error)
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	for _, key := range []string{"ok", "command", "data", "warnings"} {
		if _, exists := decoded[key]; !exists {
			t.Fatalf("missing envelope key %q in %#v", key, decoded)
		}
	}
}

func TestResponseEnvelopeError(t *testing.T) {
	resp := NewErrorResponse("wowdata bad", "not_implemented", "command is not implemented yet")

	if resp.OK {
		t.Fatalf("expected non-OK response")
	}
	if resp.Error == nil {
		t.Fatalf("expected error payload")
	}
	if resp.Error.Code != "not_implemented" {
		t.Fatalf("error code = %q", resp.Error.Code)
	}
}

func TestRootHelp(t *testing.T) {
	stdout, stderr, err := executeCommand(t, "--help")
	if err != nil {
		t.Fatalf("help returned error: %v stderr=%s", err, stderr)
	}
	for _, want := range []string{
		"wowdata",
		"warmup",
		"db2",
		"golden",
		"Examples:",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("root help missing %q:\n%s", want, stdout)
		}
	}
}

func TestAllPlannedCommandHelp(t *testing.T) {
	commands := [][]string{
		{"warmup"},
		{"db2"}, {"db2", "schema"}, {"db2", "rows"}, {"db2", "search"}, {"db2", "foreign-key"}, {"db2", "stream"},
		{"spell"}, {"spell", "info"}, {"spell", "auras"}, {"spell", "summons"},
		{"encounter"}, {"encounter", "get"},
		{"file"}, {"file", "lookup"}, {"file", "search"}, {"file", "extension"}, {"file", "get"}, {"file", "exists"}, {"file", "encoding"}, {"file", "export"},
		{"icon"}, {"icon", "export"},
		{"casc"}, {"casc", "info"}, {"casc", "products"}, {"casc", "diagnose"},
		{"item"}, {"item", "get"}, {"item", "models"}, {"item", "geosets"}, {"item", "textures"},
		{"creature"}, {"creature", "display"}, {"creature", "model"},
		{"decor"}, {"decor", "list"}, {"decor", "get"},
		{"video"}, {"video", "demux"},
		{"golden"}, {"golden", "capture"}, {"golden", "compare"},
	}

	for _, parts := range commands {
		args := append(append([]string{}, parts...), "--help")
		stdout, stderr, err := executeCommand(t, args...)
		if err != nil {
			t.Fatalf("%v --help returned error: %v stderr=%s", parts, err, stderr)
		}
		if !strings.Contains(stdout, "Usage:") {
			t.Fatalf("%v --help missing usage:\n%s", parts, stdout)
		}
		if !strings.Contains(stdout, "Examples:") {
			t.Fatalf("%v --help missing examples:\n%s", parts, stdout)
		}
	}
}
