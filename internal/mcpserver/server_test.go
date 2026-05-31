package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestServerInitializeAndListTools(t *testing.T) {
	server := NewServer("wowdata-test", []Tool{
		{
			Name:        "wow_ping",
			Description: "Ping tool",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
				return map[string]interface{}{"pong": true}, nil
			},
		},
	})

	stdout := runMCPServer(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
	)

	if !strings.Contains(stdout, `"protocolVersion":"2024-11-05"`) {
		t.Fatalf("initialize response missing protocol version:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"name":"wow_ping"`) {
		t.Fatalf("tools/list response missing tool:\n%s", stdout)
	}
}

func TestServerCallsTool(t *testing.T) {
	server := NewServer("wowdata-test", []Tool{
		{
			Name:        "wow_echo",
			Description: "Echo tool",
			InputSchema: map[string]interface{}{"type": "object"},
			Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
				var payload map[string]interface{}
				if err := json.Unmarshal(args, &payload); err != nil {
					return nil, err
				}
				return map[string]interface{}{"echo": payload["message"]}, nil
			},
		},
	})

	stdout := runMCPServer(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wow_echo","arguments":{"message":"hi"}}}`,
	)

	if !strings.Contains(stdout, `"type":"text"`) || !strings.Contains(stdout, `\"echo\":\"hi\"`) {
		t.Fatalf("tools/call response missing text JSON content:\n%s", stdout)
	}
}

func TestServerReportsUnknownTool(t *testing.T) {
	server := NewServer("wowdata-test", nil)

	stdout := runMCPServer(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"missing","arguments":{}}}`,
	)

	if !strings.Contains(stdout, `"code":-32602`) || !strings.Contains(stdout, "unknown tool") {
		t.Fatalf("unknown tool response:\n%s", stdout)
	}
}

func TestServerIgnoresBlankLinesBetweenFrames(t *testing.T) {
	server := NewServer("wowdata-test", []Tool{
		{
			Name:        "wow_ping",
			Description: "Ping tool",
			InputSchema: map[string]interface{}{"type": "object"},
			Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
				return map[string]interface{}{"pong": true}, nil
			},
		},
	})
	var stdin bytes.Buffer
	writeFrame(&stdin, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wow_ping","arguments":{}}}`))
	stdin.WriteString("\r\n")
	var stdout bytes.Buffer

	if err := server.Serve(context.Background(), &stdin, &stdout); err != nil {
		t.Fatalf("Serve should ignore trailing blank line: %v", err)
	}
	if !strings.Contains(stdout.String(), `\"pong\":true`) {
		t.Fatalf("missing tool result:\n%s", stdout.String())
	}
}

func runMCPServer(t *testing.T, server *Server, messages ...string) string {
	t.Helper()
	var stdin bytes.Buffer
	for _, msg := range messages {
		writeFrame(&stdin, []byte(msg))
	}
	var stdout bytes.Buffer
	if err := server.Serve(context.Background(), &stdin, &stdout); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	return stdout.String()
}
