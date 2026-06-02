package mcp

import (
	"context"
	"encoding/json"

	"wowdata/internal/shared/mcpserver"
)

var stdioToolNames = []string{
	"wow_warmup",
	"wow_casc",
	"wow_query",
	"wow_file",
	"wow_icon",
	"wow_spell",
	"wow_encounter",
	"wow_item",
	"wow_creature",
	"wow_decor",
	"wow_video",
}

func StdioToolNames() []string {
	return append([]string{}, stdioToolNames...)
}

type ToolHandler func(context.Context, json.RawMessage) (interface{}, error)

type StdioHandlerFactory func(name string) ToolHandler

func StdioTools(factory StdioHandlerFactory) []mcpserver.Tool {
	tools := make([]mcpserver.Tool, 0, len(stdioToolNames))
	for _, name := range stdioToolNames {
		toolName := name
		tools = append(tools, mcpserver.Tool{
			Name:        toolName,
			Description: stdioToolDescription(toolName),
			InputSchema: objectSchema(),
			Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
				return factory(toolName)(ctx, raw)
			},
		})
	}
	return tools
}

func stdioToolDescription(name string) string {
	switch name {
	case "wow_warmup":
		return "Initialize local or remote WoW data context."
	case "wow_casc":
		return "Inspect CASC source state."
	case "wow_query":
		return "Query DB2 tables."
	case "wow_file":
		return "Query and export CASC files."
	case "wow_icon":
		return "Export BLP icons."
	case "wow_spell":
		return "Inspect spell relationships."
	case "wow_encounter":
		return "Query JournalEncounter data."
	case "wow_item":
		return "Query item metadata and assets."
	case "wow_creature":
		return "Query creature displays and models."
	case "wow_decor":
		return "Query decor data."
	case "wow_video":
		return "Process video container data."
	default:
		return name
	}
}
