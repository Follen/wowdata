package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestHTTPHandlerAcceptsInitializedNotification(t *testing.T) {
	server := NewServer("wowdata-test", nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted && rec.Code != http.StatusNoContent {
		t.Fatalf("initialized notification status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "method not found") {
		t.Fatalf("notification should not return JSON-RPC method error: %s", rec.Body.String())
	}
}

func TestHTTPHandlerInitializesAndListsTools(t *testing.T) {
	server := NewServer("wowdata-test", []Tool{{
		Name:        "wow_ping",
		Description: "Ping tool",
		InputSchema: map[string]interface{}{"type": "object"},
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return map[string]interface{}{"pong": true}, nil
		},
	}})
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"initialize", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, `"serverInfo"`},
		{"tools-list", `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`, `"name":"wow_ping"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			server.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("response missing %s: %s", tc.want, rec.Body.String())
			}
		})
	}
}

func TestHTTPHandlerRejectsSSEGet(t *testing.T) {
	server := NewServer("wowdata-test", nil)
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("SSE GET status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "streamable_http") {
		t.Fatalf("SSE rejection should explain supported transport: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"/mcp"`) {
		t.Fatalf("SSE rejection should report /mcp endpoint: %s", rec.Body.String())
	}
}

func TestServerCallToolReturnsStructuredContentAndResourceLinks(t *testing.T) {
	server := NewServer("wowdata-test", []Tool{{
		Name:        "wow_export",
		Description: "Export",
		InputSchema: map[string]interface{}{"type": "object"},
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return map[string]interface{}{
				"ok": true,
				"data": map[string]interface{}{
					"path":     `C:\tmp\icon.png`,
					"uri":      "file:///C:/tmp/icon.png",
					"name":     "icon.png",
					"mimeType": "image/png",
					"size":     42,
				},
			}, nil
		},
	}})
	stdout := runMCPServer(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wow_export","arguments":{}}}`,
	)

	if !strings.Contains(stdout, `"structuredContent"`) {
		t.Fatalf("tool result should include structuredContent:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"type":"resource_link"`) || !strings.Contains(stdout, `"uri":"file:///C:/tmp/icon.png"`) {
		t.Fatalf("tool result should include resource_link for exported artifact:\n%s", stdout)
	}
}

func TestServerCallToolReturnsHTTPResourceLinks(t *testing.T) {
	server := NewServer("wowdata-test", []Tool{{
		Name:        "wow_export",
		Description: "Export",
		InputSchema: map[string]interface{}{"type": "object"},
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return map[string]interface{}{
				"ok": true,
				"data": map[string]interface{}{
					"uri":      "https://mcp.example.com/files/icons/134400.png",
					"name":     "134400.png",
					"mimeType": "image/png",
					"size":     42,
				},
			}, nil
		},
	}})
	stdout := runMCPServer(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wow_export","arguments":{}}}`,
	)

	if !strings.Contains(stdout, `"type":"resource_link"`) || !strings.Contains(stdout, `"uri":"https://mcp.example.com/files/icons/134400.png"`) {
		t.Fatalf("tool result should include resource_link for HTTP artifact:\n%s", stdout)
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
