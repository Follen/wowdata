package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

const protocolVersion = "2024-11-05"

type Tool struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     func(context.Context, json.RawMessage) (interface{}, error)
}

type Server struct {
	name  string
	tools map[string]Tool
}

func NewServer(name string, tools []Tool) *Server {
	byName := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	return &Server{name: name, tools: byName}
}

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	for {
		payload, err := readFrame(reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		resp, ok := s.handle(ctx, payload)
		if !ok {
			continue
		}
		data, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		if err := writeFrame(out, data); err != nil {
			return err
		}
	}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) handle(ctx context.Context, payload []byte) (rpcResponse, bool) {
	var req rpcRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: err.Error()}}, true
	}
	if req.ID == nil {
		return rpcResponse{}, false
	}
	switch req.Method {
	case "initialize":
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    s.name,
				"version": "0.1.0",
			},
		}}, true
	case "tools/list":
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"tools": s.toolList()}}, true
	case "tools/call":
		return s.callTool(ctx, req), true
	default:
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found"}}, true
	}
}

func (s *Server) toolList() []map[string]interface{} {
	names := make([]string, 0, len(s.tools))
	for name := range s.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		tool := s.tools[name]
		schema := tool.InputSchema
		if schema == nil {
			schema = map[string]interface{}{"type": "object"}
		}
		out = append(out, map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": schema,
		})
	}
	return out
}

func (s *Server) callTool(ctx context.Context, req rpcRequest) rpcResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32602, Message: err.Error()}}
	}
	tool, ok := s.tools[params.Name]
	if !ok {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32602, Message: "unknown tool: " + params.Name}}
	}
	if len(params.Arguments) == 0 {
		params.Arguments = []byte("{}")
	}
	result, err := tool.Handler(ctx, params.Arguments)
	if err != nil {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32000, Message: err.Error()}}
	}
	text, err := json.Marshal(result)
	if err != nil {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32000, Message: err.Error()}}
	}
	return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": string(text)}},
	}}
}

func readFrame(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if contentLength < 0 {
				continue
			}
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("invalid MCP header: %s", line)
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeFrame(out io.Writer, payload []byte) error {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n", len(payload))
	buf.Write(payload)
	_, err := out.Write(buf.Bytes())
	return err
}
