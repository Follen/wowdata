package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestStdioToolNamesIncludeWarmupAndExistingTools(t *testing.T) {
	names := stringSet(StdioToolNames())

	for _, want := range []string{"wow_warmup", "wow_db2", "wow_icon"} {
		if !names[want] {
			t.Fatalf("StdioToolNames missing %q; got %#v", want, names)
		}
	}
}

func TestStdioToolsBuildCLIBridgeTools(t *testing.T) {
	called := false
	tools := StdioTools(func(name string) ToolHandler {
		if name == "wow_db2" {
			return func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
				called = true
				return map[string]interface{}{"ok": true, "command": "db2"}, nil
			}
		}
		return func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			return map[string]interface{}{"ok": false}, nil
		}
	})

	tool := findTool(t, tools, "wow_db2")
	result, err := tool.Handler(context.Background(), json.RawMessage(`{"table":"SpellName"}`))
	if err != nil {
		t.Fatalf("stdio tool handler: %v", err)
	}
	if !called {
		t.Fatal("stdio tool did not call CLI bridge handler")
	}
	if result.(map[string]interface{})["command"] != "db2" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}
