package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"wowdata/internal/mcpserver"

	"github.com/spf13/cobra"
)

type mcpTool = mcpserver.Tool

func registerMCPCommand(root *cobra.Command, rt *Runtime) {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run wowdata as an MCP server.",
	}
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve MCP tools over stdio.",
		RunE: func(cmd *cobra.Command, args []string) error {
			server := newMCPServerForRuntime(rt)
			return server.Serve(cmd.Context(), os.Stdin, os.Stdout)
		},
	}
	mcpCmd.AddCommand(serveCmd)
	root.AddCommand(mcpCmd)
}

func newMCPServerForRuntime(rt *Runtime) *mcpserver.Server {
	return mcpserver.NewServer("wowdata", mcpToolsForRuntime(rt))
}

func mcpToolsForRuntime(rt *Runtime) []mcpTool {
	return []mcpTool{
		cliTool(rt, "wow_warmup", "Initialize local or remote WoW data context.", []string{"warmup"}, warmupArgs),
		cliTool(rt, "wow_casc", "Inspect CASC source state.", []string{"casc"}, cascArgs),
		cliTool(rt, "wow_db2", "Query DB2 tables.", []string{"db2"}, db2Args),
		cliTool(rt, "wow_file", "Query and export CASC files.", []string{"file"}, fileArgs),
		cliTool(rt, "wow_icon", "Export BLP icons.", []string{"icon", "export"}, iconArgs),
		cliTool(rt, "wow_spell", "Inspect spell relationships.", []string{"spell"}, spellArgs),
		cliTool(rt, "wow_encounter", "Query JournalEncounter data.", []string{"encounter", "get"}, encounterArgs),
		cliTool(rt, "wow_item", "Query item metadata and assets.", []string{"item"}, itemArgs),
		cliTool(rt, "wow_creature", "Query creature displays and models.", []string{"creature"}, creatureArgs),
		cliTool(rt, "wow_decor", "Query decor data.", []string{"decor"}, decorArgs),
		cliTool(rt, "wow_video", "Process video container data.", []string{"video", "demux"}, videoArgs),
	}
}

func cliTool(rt *Runtime, name, description string, base []string, mapper func(map[string]interface{}) ([]string, error)) mcpTool {
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
			return executeCLIJSON(ctx, rt, append(append([]string{}, base...), extra...))
		},
	}
}

func executeCLIJSON(ctx context.Context, rt *Runtime, args []string) (interface{}, error) {
	cmd := newRootCommandForRuntime(rt)
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
	mode := stringArg(args, "mode", "lookup")
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
	out := []string{}
	addNumberFlag(&out, args, "fileDataID", "--file-data-id")
	addStringFlag(&out, args, "format", "--format")
	addNumberFlag(&out, args, "mipmap", "--mipmap")
	addNumberFlag(&out, args, "mask", "--mask")
	addStringFlag(&out, args, "output", "--output")
	return out, nil
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
