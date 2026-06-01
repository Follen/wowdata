package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestStdioToolNamesIncludeWarmupAndExistingTools(t *testing.T) {
	names := stringSet(StdioToolNames())

	for _, want := range []string{"wow_warmup", "wow_query", "wow_icon"} {
		if !names[want] {
			t.Fatalf("StdioToolNames missing %q; got %#v", want, names)
		}
	}
	legacyName := "wow_" + "db2"
	if names[legacyName] {
		t.Fatalf("StdioToolNames should not expose %s; got %#v", legacyName, names)
	}
}

func TestStdioToolsBuildCLIBridgeTools(t *testing.T) {
	called := false
	tools := StdioTools(func(name string) ToolHandler {
		if name == "wow_query" {
			return func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
				called = true
				return map[string]interface{}{"ok": true, "command": "query"}, nil
			}
		}
		return func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			return map[string]interface{}{"ok": false}, nil
		}
	})

	tool := findTool(t, tools, "wow_query")
	result, err := tool.Handler(context.Background(), json.RawMessage(`{"table":"SpellName"}`))
	if err != nil {
		t.Fatalf("stdio tool handler: %v", err)
	}
	if !called {
		t.Fatal("stdio tool did not call CLI bridge handler")
	}
	if result.(map[string]interface{})["command"] != "query" {
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
