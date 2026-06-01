package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	mcpadapter "wowdata/internal/adapter/mcp"
	"wowdata/internal/artifact"
	"wowdata/internal/config"
	"wowdata/internal/listfile"
	"wowdata/internal/mcpserver"
	appruntime "wowdata/internal/runtime"
	httpservice "wowdata/internal/service/http"

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

	mcpCmd.AddCommand(newMCPStdioCommand(rt), newMCPHTTPCommand(rt))
	root.AddCommand(mcpCmd)
}

func newMCPServerForRuntime(rt *Runtime) *mcpserver.Server {
	return newMCPServerForRuntimeWithArtifacts(rt, artifactConfig{})
}

func newMCPServerForRuntimeWithArtifacts(rt *Runtime, artifacts artifactConfig) *mcpserver.Server {
	return mcpserver.NewServer("wowdata", mcpToolsForRuntimeWithArtifacts(rt, artifacts))
}

func newMCPHTTPServerForService(svc *httpservice.Service, rt *Runtime, artifacts artifactConfig) *mcpserver.Server {
	return mcpserver.NewServer("wowdata", mcpadapter.HTTPTools(svc, mcpadapter.HTTPToolOptions{
		ExposeAdmin: svc.ToolPolicy().ExposeAdminTools,
		Artifacts:   newMCPArtifactReserver(artifacts),
		Assets:      httpRuntimeAssets{rt: rt, svc: svc},
	}))
}

func registerMCPHTTPHandlers(mux *http.ServeMux, server *mcpserver.Server, baseURL, cacheRoot, artifactRoot string) {
	mux.Handle("/mcp", server)
	mux.Handle("/mcp/", server)
	if artifactRoot != "" {
		mux.HandleFunc("/files/", artifactFileHandler(artifactRoot))
	}
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeHelpJSON(w, http.StatusOK, map[string]interface{}{
			"ok":        true,
			"service":   "wowdata-mcp",
			"endpoint":  publicURL(baseURL, "/mcp"),
			"transport": "streamable_http",
			"cacheRoot": filepath.ToSlash(cacheRoot),
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

func artifactFileHandler(root string) http.HandlerFunc {
	root = filepath.Clean(root)
	return func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/files/")
		if rel == "" {
			http.NotFound(w, r)
			return
		}
		clean := filepath.Clean(filepath.FromSlash(rel))
		if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			http.NotFound(w, r)
			return
		}
		filePath := filepath.Join(root, clean)
		if !pathInsideRoot(root, filePath) {
			http.NotFound(w, r)
			return
		}
		if realPath, err := filepath.EvalSymlinks(filePath); err == nil && !pathInsideRoot(root, realPath) {
			http.NotFound(w, r)
			return
		}
		info, err := os.Stat(filePath)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filePath)
	}
}

func pathInsideRoot(root, filePath string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(absRoot), filepath.Clean(absPath))
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
	escapedEndpoint := htmltemplate.HTMLEscapeString(endpoint)
	jsonEndpoint, _ := json.Marshal(endpoint)
	escapedJSONEndpoint := htmltemplate.HTMLEscapeString(string(jsonEndpoint))
	return fmt.Sprintf(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>wowdata MCP Help</title>
<style>body{font-family:Segoe UI,Arial,sans-serif;line-height:1.55;max-width:980px;margin:40px auto;padding:0 18px;color:#172033}pre{background:#0f172a;color:#dbeafe;padding:14px;border-radius:8px;overflow:auto}code{background:#e5e7eb;padding:2px 5px;border-radius:4px}h1,h2{color:#0f172a}li{margin:4px 0}</style></head>
<body><h1>wowdata MCP</h1><p>Endpoint: <code>%s</code></p>
<h2>Codex HTTP config</h2><pre>codex mcp add wowdata --url %s</pre>
<h2>Claude Code HTTP config</h2><pre>claude mcp add --transport http wowdata %s</pre>
<h2>cc-switch HTTP config</h2><pre>{
  "type": "http",
  "url": %s
}</pre>
<h2>Local stdio fallback</h2><pre>claude mcp add --transport stdio wowdata -- wowdata mcp stdio
wowdata mcp stdio</pre>
<h2>HTTP supported tools</h2><p>wow_builds, wow_status, wow_db2, wow_item, wow_spell, wow_file, wow_icon, wow_creature, wow_encounter, wow_decor, wow_video.</p>
<h2>Admin tools when enabled</h2><p>Admin tools are hidden by default and appear only when <code>tools.expose_admin_tools</code> is enabled: wow_refresh_builds, wow_prepare, wow_prune_cache.</p>
<h2>Artifact download behavior</h2><p>Tools that create files return public artifact URLs when the server has an artifact root and base URL configured; local file URIs are preserved as <code>fileURI</code> when rewritten.</p>
</body></html>`, escapedEndpoint, escapedEndpoint, escapedEndpoint, escapedJSONEndpoint)
}

func mcpToolsForRuntime(rt *Runtime) []mcpTool {
	return mcpToolsForRuntimeWithArtifacts(rt, artifactConfig{})
}

func mcpToolsForRuntimeWithArtifacts(rt *Runtime, artifacts artifactConfig) []mcpTool {
	return mcpadapter.StdioTools(func(name string) mcpadapter.ToolHandler {
		return stdioCLIHandler(rt, name, artifacts)
	})
}

type artifactConfig struct {
	root    string
	baseURL string
}

type mcpArtifactReserver struct {
	manager *artifact.Manager
}

type httpRuntimeAssets struct {
	rt  *Runtime
	svc *httpservice.Service
}

func (a httpRuntimeAssets) FileStore(ctx context.Context, rc httpservice.RequestContext, needListfile bool) (appruntime.FileStore, error) {
	lf, reader, err := a.prepare(ctx, rc, needListfile)
	if err != nil {
		return nil, err
	}
	return appruntime.NewCASCFileStore(lf, nil, reader), nil
}

func (a httpRuntimeAssets) IconStore(ctx context.Context, rc httpservice.RequestContext) (appruntime.IconStore, error) {
	_, reader, err := a.prepare(ctx, rc, false)
	if err != nil {
		return nil, err
	}
	return appruntime.NewCASCFileStore(nil, nil, reader), nil
}

func (a httpRuntimeAssets) prepare(ctx context.Context, rc httpservice.RequestContext, needListfile bool) (*listfile.Listfile, appruntime.FileDataReader, error) {
	if a.rt == nil {
		return nil, nil, fmt.Errorf("runtime is required")
	}
	if a.svc != nil {
		rc = a.svc.ResolveRequestContext(rc)
	} else {
		rc = httpservice.NewService(config.DefaultHTTPConfig(), nil).ResolveRequestContext(rc)
	}
	a.rt.httpRuntimeMu.Lock()
	defer a.rt.httpRuntimeMu.Unlock()
	_, err := a.rt.initialize(warmupOptions{
		Source:          "remote",
		Region:          rc.Region,
		Product:         rc.Product,
		Locale:          rc.Locale,
		CacheRoot:       a.rt.CacheRoot,
		WarmDBDManifest: false,
		WarmListfile:    needListfile,
	})
	if err != nil {
		return nil, nil, err
	}
	a.rt.mu.Lock()
	defer a.rt.mu.Unlock()
	var reader appruntime.FileDataReader
	if a.rt.CASC != nil {
		reader = a.rt.CASC
	} else if a.rt.Local != nil {
		reader = a.rt.Local
	}
	return a.rt.LF, reader, nil
}

func newMCPArtifactReserver(cfg artifactConfig) *mcpArtifactReserver {
	if cfg.root == "" {
		return nil
	}
	return &mcpArtifactReserver{manager: artifact.NewManager(artifact.Config{Root: cfg.root, BaseURL: cfg.baseURL})}
}

func (r *mcpArtifactReserver) LinkArtifact(path, mimeType string) (mcpadapter.ArtifactLink, error) {
	link, err := r.manager.LinkForPath(path, mimeType)
	if err != nil {
		return mcpadapter.ArtifactLink{}, err
	}
	return mcpadapter.ArtifactLink{
		Path:        link.Path,
		URI:         link.URI,
		DownloadURL: link.DownloadURL,
		MimeType:    link.MimeType,
		Name:        link.Name,
		Size:        link.Size,
		SHA256:      link.SHA256,
	}, nil
}

func (r *mcpArtifactReserver) ReserveArtifact(kind, name, mimeType string) (string, mcpadapter.ArtifactLink, error) {
	outputPath, link, err := r.manager.Reserve(kind, name, mimeType)
	if err != nil {
		return "", mcpadapter.ArtifactLink{}, err
	}
	return outputPath, mcpadapter.ArtifactLink{
		Path:        link.Path,
		URI:         link.URI,
		DownloadURL: link.DownloadURL,
		MimeType:    link.MimeType,
		Name:        link.Name,
		Size:        link.Size,
		SHA256:      link.SHA256,
	}, nil
}

func stdioCLIHandler(rt *Runtime, name string, artifacts artifactConfig) mcpadapter.ToolHandler {
	switch name {
	case "wow_warmup":
		return cliTool(rt, name, "", []string{"warmup"}, warmupArgs, artifacts).Handler
	case "wow_casc":
		return cliTool(rt, name, "", []string{"casc"}, cascArgs, artifacts).Handler
	case "wow_db2":
		return cliTool(rt, name, "", []string{"db2"}, db2Args, artifacts).Handler
	case "wow_file":
		return cliTool(rt, name, "", []string{"file"}, func(args map[string]interface{}) ([]string, error) {
			return fileArgsWithArtifacts(args, artifacts)
		}, artifacts).Handler
	case "wow_icon":
		return cliTool(rt, name, "", []string{"icon", "export"}, func(args map[string]interface{}) ([]string, error) {
			return iconArgsWithArtifacts(args, artifacts)
		}, artifacts).Handler
	case "wow_spell":
		return cliTool(rt, name, "", []string{"spell"}, spellArgs, artifacts).Handler
	case "wow_encounter":
		return cliTool(rt, name, "", []string{"encounter", "get"}, encounterArgs, artifacts).Handler
	case "wow_item":
		return cliTool(rt, name, "", []string{"item"}, itemArgs, artifacts).Handler
	case "wow_creature":
		return cliTool(rt, name, "", []string{"creature"}, creatureArgs, artifacts).Handler
	case "wow_decor":
		return cliTool(rt, name, "", []string{"decor"}, decorArgs, artifacts).Handler
	case "wow_video":
		return cliTool(rt, name, "", []string{"video", "demux"}, videoArgs, artifacts).Handler
	default:
		return func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			return nil, fmt.Errorf("unknown stdio MCP tool: %s", name)
		}
	}
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
	manager := artifact.NewManager(artifact.Config{Root: artifacts.root, BaseURL: artifacts.baseURL})
	addDownloadLinks(result, manager)
	return result
}

func addDownloadLinks(value interface{}, manager *artifact.Manager) {
	switch v := value.(type) {
	case map[string]interface{}:
		addDownloadLinkToMap(v, manager)
		for _, child := range v {
			addDownloadLinks(child, manager)
		}
	case []interface{}:
		for _, child := range v {
			addDownloadLinks(child, manager)
		}
	case []map[string]interface{}:
		for _, child := range v {
			addDownloadLinks(child, manager)
		}
	}
}

func addDownloadLinkToMap(v map[string]interface{}, manager *artifact.Manager) {
	rawPath, ok := mcpStringValue(v["path"])
	if !ok || rawPath == "" {
		return
	}
	mimeType, _ := mcpStringValue(v["mimeType"])
	link, err := manager.LinkForPath(rawPath, mimeType)
	if err != nil {
		return
	}
	if uri, ok := mcpStringValue(v["uri"]); ok && strings.HasPrefix(uri, "file://") {
		v["fileURI"] = uri
	}
	v["downloadUrl"] = link.DownloadURL
	v["uri"] = link.URI
	if link.MimeType != "" {
		v["mimeType"] = link.MimeType
	}
	v["size"] = link.Size
	v["sha256"] = link.SHA256
	if _, ok := v["name"]; !ok {
		v["name"] = link.Name
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
		manager := artifact.NewManager(artifact.Config{Root: artifacts.root, BaseURL: artifacts.baseURL})
		if id, ok := args["fileDataID"]; ok {
			outputPath, _, err := manager.Reserve("files", scalarString(id)+".bin", "application/octet-stream")
			if err != nil {
				return nil, err
			}
			args = cloneArgs(args)
			args["output"] = outputPath
		} else if filename := stringArg(args, "filename", ""); filename != "" {
			outputPath, _, err := manager.Reserve("files", path.Base(filepath.ToSlash(filename)), "application/octet-stream")
			if err != nil {
				return nil, err
			}
			args = cloneArgs(args)
			args["output"] = outputPath
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
			format := stringArg(args, "format", "png")
			manager := artifact.NewManager(artifact.Config{Root: artifacts.root, BaseURL: artifacts.baseURL})
			outputPath, _, err := manager.Reserve("icons", scalarString(id)+"."+format, "image/"+format)
			if err != nil {
				return nil, err
			}
			args = cloneArgs(args)
			args["output"] = outputPath
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
