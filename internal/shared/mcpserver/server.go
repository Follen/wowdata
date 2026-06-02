package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

const (
	protocolVersion = "2024-11-05"
	serverVersion   = "0.0.1"
)

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

func (s *Server) ToolNamesForTest() []string {
	names := make([]string, 0, len(s.tools))
	for name := range s.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, MCP-Protocol-Version")
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method == http.MethodGet {
		if acceptsSSE(r.Header.Get("Accept")) {
			writeJSONHTTP(w, http.StatusMethodNotAllowed, map[string]interface{}{
				"error":     "SSE transport is not supported on this endpoint",
				"transport": "streamable_http",
				"endpoint":  "/wowdata",
			})
			return
		}
		writeJSONHTTP(w, http.StatusOK, map[string]interface{}{
			"ok":              true,
			"transport":       "streamable_http",
			"protocolVersion": protocolVersion,
			"serverInfo": map[string]interface{}{
				"name":    s.name,
				"version": serverVersion,
			},
		})
		return
	}
	if r.Method != http.MethodPost {
		writeJSONHTTP(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONHTTP(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: err.Error()}})
		return
	}
	resp, ok := s.handle(r.Context(), body)
	if !ok {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSONHTTP(w, http.StatusOK, resp)
}

func writeJSONHTTP(w http.ResponseWriter, code int, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		code = http.StatusInternalServerError
		data = []byte(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"encode response failed"}}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(code)
	_, _ = w.Write(data)
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
				"version": serverVersion,
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
	content := []map[string]interface{}{{"type": "text", "text": string(text)}}
	content = append(content, resourceLinksFrom(result)...)
	return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
		"content":           content,
		"structuredContent": result,
	}}
}

func acceptsSSE(accept string) bool {
	for _, part := range strings.Split(accept, ",") {
		mediaType := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if strings.EqualFold(mediaType, "text/event-stream") {
			return true
		}
	}
	return false
}

func resourceLinksFrom(value interface{}) []map[string]interface{} {
	links := []map[string]interface{}{}
	collectResourceLinks(value, &links)
	return links
}

func collectResourceLinks(value interface{}, links *[]map[string]interface{}) {
	switch v := value.(type) {
	case map[string]interface{}:
		if link, ok := resourceLinkFromMap(v); ok {
			*links = append(*links, link)
		}
		for _, child := range v {
			collectResourceLinks(child, links)
		}
	case []interface{}:
		for _, child := range v {
			collectResourceLinks(child, links)
		}
	case []map[string]interface{}:
		for _, child := range v {
			collectResourceLinks(child, links)
		}
	}
}

func resourceLinkFromMap(v map[string]interface{}) (map[string]interface{}, bool) {
	uri, ok := stringValue(v["uri"])
	if !ok || !isResourceURI(uri) {
		return nil, false
	}
	name, _ := stringValue(v["name"])
	if name == "" {
		name = uri
	}
	link := map[string]interface{}{
		"type": "resource_link",
		"uri":  uri,
		"name": name,
	}
	if mimeType, ok := stringValue(v["mimeType"]); ok && mimeType != "" {
		link["mimeType"] = mimeType
	}
	if size, ok := v["size"]; ok {
		link["size"] = size
	}
	return link, true
}

func isResourceURI(uri string) bool {
	return strings.HasPrefix(uri, "file://") ||
		strings.HasPrefix(uri, "http://") ||
		strings.HasPrefix(uri, "https://")
}

func stringValue(value interface{}) (string, bool) {
	s, ok := value.(string)
	return s, ok
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
