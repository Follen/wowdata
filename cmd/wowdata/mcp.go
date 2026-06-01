package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"wowdata/internal/mcpserver"

	"github.com/spf13/cobra"
)

type mcpTool = mcpserver.Tool

func registerMCPCommand(root *cobra.Command, rt *Runtime) {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run wowdata as an MCP server.",
		Long: `Run wowdata as an MCP server.

Transports:
  stdio  Local process transport for Codex, Claude Code, and cc-switch command configs.
  http   Streamable HTTP endpoint for remote services, domains, and shared deployments.

Examples:
  wowdata mcp stdio
  wowdata mcp http --host 127.0.0.1 --port 9788 --base-url http://127.0.0.1:9788`,
	}
	stdioCmd := &cobra.Command{
		Use:   "stdio",
		Short: "Serve MCP tools over stdio.",
		Long: `Serve MCP tools over stdio.

Use this for local clients that launch wowdata as a subprocess.

Codex CLI:
  codex mcp add wowdata -- wowdata mcp stdio

Claude Code:
  claude mcp add wowdata -- wowdata mcp stdio

cc-switch custom MCP:
  {
    "type": "stdio",
    "command": "wowdata",
    "args": ["mcp", "stdio"]
  }

Legacy compatibility:
  wowdata --mcp is treated as wowdata mcp stdio.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			server := newMCPServerForRuntime(rt)
			return server.Serve(cmd.Context(), os.Stdin, os.Stdout)
		},
	}
	var httpHost, httpBaseURL string
	var httpArtifactRoot, httpArtifactBaseURL string
	var httpPort int
	var httpMaxContexts int
	httpCmd := &cobra.Command{
		Use:   "http",
		Short: "Serve MCP tools over Streamable HTTP.",
		Long: `Serve MCP tools over Streamable HTTP.

Use this for remote deployments with a domain, TLS, reverse proxy, or shared service.
The MCP endpoint is /mcp. Health and agent-readable setup guidance are available at /health and /help.

Codex CLI:
  codex mcp add wowdata --url https://mcp.example.com:9443/mcp

Claude Code:
  claude mcp add --transport http wowdata https://mcp.example.com:9443/mcp

Claude Code stdio fallback:
  claude mcp add --transport stdio wowdata -- wowdata mcp stdio

cc-switch custom MCP:
  {
    "type": "http",
    "url": "https://mcp.example.com:9443/mcp"
  }

Compatibility notes:
  The HTTP server accepts GET, HEAD, OPTIONS, and POST on /mcp.
  JSON-RPC notifications such as notifications/initialized return HTTP 202 with no JSON-RPC error.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			syncRuntimeFromPersistentFlags(cmd, rt)
			rt.enableHTTPWarmupGate()
			rt.enableContextCache(httpMaxContexts)
			server := newMCPServerForRuntimeWithArtifacts(rt, artifactConfig{
				root:    httpArtifactRoot,
				baseURL: httpArtifactBaseURL,
			})
			addr := fmt.Sprintf("%s:%d", httpHost, httpPort)
			mux := http.NewServeMux()
			registerMCPHTTPHandlers(mux, server, httpBaseURL, rt)
			httpServer := &http.Server{
				Addr:              addr,
				Handler:           mux,
				ReadHeaderTimeout: 10 * time.Second,
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "wowdata MCP HTTP listening on http://%s/mcp\n", addr)
			return httpServer.ListenAndServe()
		},
	}
	httpCmd.Flags().StringVar(&httpHost, "host", "127.0.0.1", "Host/interface to bind")
	httpCmd.Flags().IntVar(&httpPort, "port", 9788, "Port to bind")
	httpCmd.Flags().StringVar(&httpBaseURL, "base-url", "", "Public base URL used in help output, such as https://mcp.example.com:9443")
	httpCmd.Flags().StringVar(&httpArtifactRoot, "artifact-root", "", "Local directory exposed by the reverse proxy for exported artifacts")
	httpCmd.Flags().StringVar(&httpArtifactBaseURL, "artifact-base-url", "", "Public base URL for exported artifacts, such as https://mcp.example.com:9443/files")
	httpCmd.Flags().IntVar(&httpMaxContexts, "max-contexts", 1, "Maximum warmed build contexts to keep in memory for HTTP MCP")

	mcpCmd.AddCommand(stdioCmd, httpCmd)
	root.AddCommand(mcpCmd)
}

func newMCPServerForRuntime(rt *Runtime) *mcpserver.Server {
	return newMCPServerForRuntimeWithArtifacts(rt, artifactConfig{})
}

func newMCPServerForRuntimeWithArtifacts(rt *Runtime, artifacts artifactConfig) *mcpserver.Server {
	return mcpserver.NewServer("wowdata", mcpToolsForRuntimeWithArtifacts(rt, artifacts))
}

func registerMCPHTTPHandlers(mux *http.ServeMux, server *mcpserver.Server, baseURL string, rt *Runtime) {
	mux.Handle("/mcp", server)
	mux.Handle("/mcp/", server)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeHelpJSON(w, http.StatusOK, map[string]interface{}{
			"ok":        true,
			"service":   "wowdata-mcp",
			"endpoint":  publicURL(baseURL, "/mcp"),
			"transport": "streamable_http",
			"cacheRoot": filepath.ToSlash(rt.CacheRoot),
		})
	})
	mux.HandleFunc("/help", func(w http.ResponseWriter, r *http.Request) {
		writeHelpHTML(w, http.StatusOK, mcpHelpHTML(baseURL))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			writeHelpHTML(w, http.StatusOK, mcpHelpHTML(baseURL))
			return
		}
		writeHelpJSON(w, http.StatusNotFound, map[string]interface{}{"error": "not found", "help": "/help", "endpoint": "/mcp"})
	})
}

func publicURL(baseURL, path string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return path
	}
	return baseURL + path
}

func writeHelpJSON(w http.ResponseWriter, code int, payload interface{}) {
	data, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(code)
	_, _ = w.Write(data)
}

func writeHelpHTML(w http.ResponseWriter, code int, text string) {
	data := []byte(text)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(code)
	_, _ = w.Write(data)
}

func mcpHelpHTML(baseURL string) string {
	endpoint := publicURL(baseURL, "/mcp")
	return fmt.Sprintf(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>wowdata MCP Help</title>
<style>body{font-family:Segoe UI,Arial,sans-serif;line-height:1.55;max-width:980px;margin:40px auto;padding:0 18px;color:#172033}pre{background:#0f172a;color:#dbeafe;padding:14px;border-radius:8px;overflow:auto}code{background:#e5e7eb;padding:2px 5px;border-radius:4px}h1,h2{color:#0f172a}</style></head>
<body><h1>wowdata MCP</h1><p>Endpoint: <code>%s</code></p>
<h2>Codex</h2><pre>codex mcp add wowdata --url %s</pre>
<h2>cc-switch</h2><pre>{
  "type": "http",
  "url": "%s"
}</pre>
<h2>Claude Code</h2><pre>claude mcp add --transport http wowdata %s</pre>
<h2>Claude Code stdio fallback</h2><pre>claude mcp add --transport stdio wowdata -- wowdata mcp stdio</pre>
<h2>Local stdio</h2><pre>wowdata mcp stdio</pre>
<p>Supported tools: wow_warmup, wow_casc, wow_db2, wow_file, wow_icon, wow_spell, wow_encounter, wow_item, wow_creature, wow_decor, wow_video.</p>
</body></html>`, endpoint, endpoint, endpoint, endpoint)
}

func mcpToolsForRuntime(rt *Runtime) []mcpTool {
	return mcpToolsForRuntimeWithArtifacts(rt, artifactConfig{})
}

func mcpToolsForRuntimeWithArtifacts(rt *Runtime, artifacts artifactConfig) []mcpTool {
	return []mcpTool{
		cliTool(rt, "wow_warmup", "Initialize local or remote WoW data context.", []string{"warmup"}, warmupArgs, artifacts),
		cliTool(rt, "wow_casc", "Inspect CASC source state.", []string{"casc"}, cascArgs, artifacts),
		cliTool(rt, "wow_db2", "Query DB2 tables.", []string{"db2"}, db2Args, artifacts),
		cliTool(rt, "wow_file", "Query and export CASC files.", []string{"file"}, func(args map[string]interface{}) ([]string, error) {
			return fileArgsWithArtifacts(args, artifacts)
		}, artifacts),
		cliTool(rt, "wow_icon", "Export BLP icons.", []string{"icon", "export"}, func(args map[string]interface{}) ([]string, error) {
			return iconArgsWithArtifacts(args, artifacts)
		}, artifacts),
		cliTool(rt, "wow_spell", "Inspect spell relationships.", []string{"spell"}, spellArgs, artifacts),
		cliTool(rt, "wow_encounter", "Query JournalEncounter data.", []string{"encounter", "get"}, encounterArgs, artifacts),
		cliTool(rt, "wow_item", "Query item metadata and assets.", []string{"item"}, itemArgs, artifacts),
		cliTool(rt, "wow_creature", "Query creature displays and models.", []string{"creature"}, creatureArgs, artifacts),
		cliTool(rt, "wow_decor", "Query decor data.", []string{"decor"}, decorArgs, artifacts),
		cliTool(rt, "wow_video", "Process video container data.", []string{"video", "demux"}, videoArgs, artifacts),
	}
}

type artifactConfig struct {
	root    string
	baseURL string
}

func cliTool(rt *Runtime, name, description string, base []string, mapper func(map[string]interface{}) ([]string, error), artifacts artifactConfig) mcpTool {
	return mcpTool{
		Name:        name,
		Description: description,
		InputSchema: map[string]interface{}{"type": "object"},
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			var args map[string]interface{}
			if len(raw) == 0 {
				args = map[string]interface{}{}
			} else if err := json.Unmarshal(raw, &args); err != nil {
				return nil, err
			}
			extra, err := mapper(args)
			if err != nil {
				return nil, err
			}
			var release func()
			if name == "wow_warmup" {
				release, err = rt.beginHTTPWarmup()
				if err != nil {
					return map[string]interface{}{
						"ok":      false,
						"command": "warmup",
						"data": map[string]interface{}{
							"status": "busy",
						},
						"warnings": []interface{}{},
						"error": map[string]interface{}{
							"code":    "warmup_in_progress",
							"message": err.Error(),
						},
					}, nil
				}
				defer release()
			}
			result, err := executeCLIJSON(ctx, rt, append(append([]string{}, base...), extra...))
			if err != nil {
				return nil, err
			}
			return addArtifactDownloadLinks(result, artifacts), nil
		},
	}
}

func addArtifactDownloadLinks(result interface{}, artifacts artifactConfig) interface{} {
	if artifacts.root == "" || artifacts.baseURL == "" {
		return result
	}
	root, err := filepath.Abs(artifacts.root)
	if err != nil {
		return result
	}
	root = filepath.Clean(root)
	addDownloadLinks(result, root, strings.TrimRight(artifacts.baseURL, "/"))
	return result
}

func addDownloadLinks(value interface{}, root, baseURL string) {
	switch v := value.(type) {
	case map[string]interface{}:
		addDownloadLinkToMap(v, root, baseURL)
		for _, child := range v {
			addDownloadLinks(child, root, baseURL)
		}
	case []interface{}:
		for _, child := range v {
			addDownloadLinks(child, root, baseURL)
		}
	case []map[string]interface{}:
		for _, child := range v {
			addDownloadLinks(child, root, baseURL)
		}
	}
}

func addDownloadLinkToMap(v map[string]interface{}, root, baseURL string) {
	rawPath, ok := mcpStringValue(v["path"])
	if !ok || rawPath == "" {
		return
	}
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return
	}
	publicURL := baseURL + "/" + path.Join(strings.Split(filepath.ToSlash(rel), "/")...)
	if uri, ok := mcpStringValue(v["uri"]); ok && strings.HasPrefix(uri, "file://") {
		v["fileURI"] = uri
	}
	v["downloadUrl"] = publicURL
	v["uri"] = publicURL
	if _, ok := v["name"]; !ok {
		v["name"] = path.Base(publicURL)
	}
}

func mcpStringValue(value interface{}) (string, bool) {
	s, ok := value.(string)
	return s, ok
}

func executeCLIJSON(ctx context.Context, rt *Runtime, args []string) (interface{}, error) {
	cmd := newRootCommandForRuntime(rt)
	applyRuntimePersistentFlags(cmd, rt)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	cmd.SetContext(ctx)
	if err := cmd.Execute(); err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}
	var decoded interface{}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		return nil, fmt.Errorf("decode CLI JSON for %v: %w; stdout=%s", args, err, stdout.String())
	}
	return decoded, nil
}

func syncRuntimeFromPersistentFlags(cmd *cobra.Command, rt *Runtime) {
	cacheRoot, _ := commandStringFlag(cmd, "cache")
	if cacheRoot != "" {
		rt.CacheRoot = resolveCacheRoot(cacheRoot, os.Executable)
	}
}

func applyRuntimePersistentFlags(cmd *cobra.Command, rt *Runtime) {
	if rt.CacheRoot == "" {
		return
	}
	_ = cmd.PersistentFlags().Set("cache", rt.CacheRoot)
}

func warmupArgs(args map[string]interface{}) ([]string, error) {
	out := []string{}
	addStringFlag(&out, args, "source", "--source")
	addStringFlag(&out, args, "path", "--path")
	addStringFlag(&out, args, "region", "--region")
	addStringFlag(&out, args, "product", "--product")
	addStringFlag(&out, args, "locale", "--locale")
	addStringFlag(&out, args, "tables", "--tables")
	addBoolFlag(&out, args, "listfile", "--listfile")
	addStringFlag(&out, args, "listfileFormat", "--listfile-format")
	addBoolFlag(&out, args, "dbdManifest", "--dbd-manifest")
	return out, nil
}

func cascArgs(args map[string]interface{}) ([]string, error) {
	mode := stringArg(args, "mode", "info")
	out := []string{mode}
	if mode == "products" {
		addStringFlag(&out, args, "source", "--source")
		addStringFlag(&out, args, "path", "--path")
		addStringFlag(&out, args, "region", "--region")
	}
	return out, nil
}

func db2Args(args map[string]interface{}) ([]string, error) {
	mode := stringArg(args, "mode", "rows")
	table := stringArg(args, "table", "")
	if table == "" {
		return nil, fmt.Errorf("table is required")
	}
	out := []string{mode, table}
	addIDsFlag(&out, args, "ids", "--ids")
	addStringFlag(&out, args, "id", "--id")
	addStringFlag(&out, args, "fields", "--fields")
	addStringFlag(&out, args, "filter", "--filter")
	addStringFlag(&out, args, "field", "--field")
	addStringFlag(&out, args, "query", "--query")
	addStringFlag(&out, args, "format", "--format")
	addNumberFlag(&out, args, "value", "--value")
	addNumberFlag(&out, args, "limit", "--limit")
	return out, nil
}

func fileArgs(args map[string]interface{}) ([]string, error) {
	return fileArgsWithArtifacts(args, artifactConfig{})
}

func fileArgsWithArtifacts(args map[string]interface{}, artifacts artifactConfig) ([]string, error) {
	mode := stringArg(args, "mode", "lookup")
	if artifacts.root != "" && stringArg(args, "output", "") == "" && (mode == "get" || mode == "export") {
		if id, ok := args["fileDataID"]; ok {
			args = cloneArgs(args)
			args["output"] = filepath.Join(artifacts.root, "files", scalarString(id)+".bin")
		} else if filename := stringArg(args, "filename", ""); filename != "" {
			args = cloneArgs(args)
			args["output"] = filepath.Join(artifacts.root, "files", path.Base(filepath.ToSlash(filename)))
		}
	}
	out := []string{mode}
	addNumberFlag(&out, args, "fileDataID", "--file-data-id")
	addStringFlag(&out, args, "filename", "--filename")
	addStringFlag(&out, args, "query", "--query")
	addStringFlag(&out, args, "extension", "--extension")
	addNumberFlag(&out, args, "limit", "--limit")
	addStringFlag(&out, args, "output", "--output")
	return out, nil
}

func iconArgs(args map[string]interface{}) ([]string, error) {
	return iconArgsWithArtifacts(args, artifactConfig{})
}

func iconArgsWithArtifacts(args map[string]interface{}, artifacts artifactConfig) ([]string, error) {
	if artifacts.root != "" && stringArg(args, "output", "") == "" {
		if id, ok := args["fileDataID"]; ok {
			args = cloneArgs(args)
			format := stringArg(args, "format", "png")
			args["output"] = filepath.Join(artifacts.root, "icons", scalarString(id)+"."+format)
		}
	}
	out := []string{}
	addNumberFlag(&out, args, "fileDataID", "--file-data-id")
	addStringFlag(&out, args, "format", "--format")
	addNumberFlag(&out, args, "mipmap", "--mipmap")
	addNumberFlag(&out, args, "mask", "--mask")
	addStringFlag(&out, args, "output", "--output")
	return out, nil
}

func cloneArgs(args map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(args)+1)
	for key, value := range args {
		out[key] = value
	}
	return out
}

func spellArgs(args map[string]interface{}) ([]string, error) {
	mode := stringArg(args, "mode", "info")
	out := []string{mode}
	addNumberFlag(&out, args, "spellID", "--spell-id")
	addNumberFlag(&out, args, "maxDepth", "--max-depth")
	addNumberFlag(&out, args, "npcID", "--npc-id")
	return out, nil
}

func encounterArgs(args map[string]interface{}) ([]string, error) {
	out := []string{}
	addNumberFlag(&out, args, "journalEncounterID", "--journal-encounter-id")
	return out, nil
}

func itemArgs(args map[string]interface{}) ([]string, error) {
	mode := stringArg(args, "mode", "get")
	out := []string{mode}
	addNumberFlag(&out, args, "itemID", "--item-id")
	addNumberFlag(&out, args, "raceID", "--race-id")
	addNumberFlag(&out, args, "gender", "--gender")
	return out, nil
}

func creatureArgs(args map[string]interface{}) ([]string, error) {
	mode := stringArg(args, "mode", "display")
	out := []string{mode}
	addNumberFlag(&out, args, "displayID", "--display-id")
	addNumberFlag(&out, args, "fileDataID", "--file-data-id")
	return out, nil
}

func decorArgs(args map[string]interface{}) ([]string, error) {
	mode := stringArg(args, "mode", "list")
	out := []string{mode}
	addNumberFlag(&out, args, "id", "--id")
	addNumberFlag(&out, args, "modelFileDataID", "--model-file-data-id")
	addNumberFlag(&out, args, "limit", "--limit")
	return out, nil
}

func videoArgs(args map[string]interface{}) ([]string, error) {
	out := []string{}
	addStringFlag(&out, args, "input", "--input")
	addStringFlag(&out, args, "output", "--output")
	return out, nil
}

func addStringFlag(out *[]string, args map[string]interface{}, key, flag string) {
	value := stringArg(args, key, "")
	if value != "" {
		*out = append(*out, flag, value)
	}
}

func addBoolFlag(out *[]string, args map[string]interface{}, key, flag string) {
	value, ok := args[key].(bool)
	if ok {
		*out = append(*out, flag+"="+strconv.FormatBool(value))
	}
}

func addNumberFlag(out *[]string, args map[string]interface{}, key, flag string) {
	if value, ok := args[key]; ok {
		*out = append(*out, flag, scalarString(value))
	}
}

func addIDsFlag(out *[]string, args map[string]interface{}, key, flag string) {
	value, ok := args[key]
	if !ok {
		return
	}
	switch v := value.(type) {
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, scalarString(item))
		}
		*out = append(*out, flag, strings.Join(parts, ","))
	default:
		*out = append(*out, flag, scalarString(v))
	}
}

func stringArg(args map[string]interface{}, key, fallback string) string {
	if value, ok := args[key]; ok {
		if s, ok := value.(string); ok {
			return s
		}
		return scalarString(value)
	}
	return fallback
}

func scalarString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatUint(uint64(v), 10)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprint(v)
	}
}
