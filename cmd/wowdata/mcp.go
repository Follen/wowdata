package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	mcpadapter "wowdata/internal/adapter/mcp"
	"wowdata/internal/shared/artifact"
	"wowdata/internal/shared/mcpserver"

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

Examples:
  wowdata mcp stdio`,
	}

	mcpCmd.AddCommand(newMCPStdioCommand(rt))
	root.AddCommand(mcpCmd)
}

func newMCPServerForRuntime(rt *Runtime) *mcpserver.Server {
	return newMCPServerForRuntimeWithArtifacts(rt, artifactConfig{})
}

func newMCPServerForRuntimeWithArtifacts(rt *Runtime, artifacts artifactConfig) *mcpserver.Server {
	return mcpserver.NewServer("wowdata", mcpToolsForRuntimeWithArtifacts(rt, artifacts))
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

func stdioCLIHandler(rt *Runtime, name string, artifacts artifactConfig) mcpadapter.ToolHandler {
	switch name {
	case "wow_warmup":
		return cliTool(rt, name, "", []string{"warmup"}, warmupArgs, artifacts).Handler
	case "wow_casc":
		return cliTool(rt, name, "", []string{"casc"}, cascArgs, artifacts).Handler
	case "wow_query":
		return cliTool(rt, name, "", []string{"query"}, db2Args, artifacts).Handler
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
