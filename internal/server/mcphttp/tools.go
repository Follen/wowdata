package mcphttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"wowdata/internal/mcpserver"
	"wowdata/internal/server/health"
	"wowdata/internal/server/service"
)

type Options struct {
	HealthProvider health.Provider
	QueryService   service.QueryService
}

func HTTPTools(opts Options) []mcpserver.Tool {
	queryService := opts.QueryService
	if queryService == nil {
		queryService = service.UnavailableQueryService{}
	}

	return []mcpserver.Tool{
		statusTool(opts.HealthProvider),
		capabilityTool("wow_builds", "List prepared build contexts.", "builds"),
		queryTool(queryService),
		capabilityTool("wow_item", "Query item metadata and assets.", "item"),
		capabilityTool("wow_spell", "Inspect spell relationships.", "spell"),
		capabilityTool("wow_file", "Query and export CASC files.", "file"),
		capabilityTool("wow_icon", "Export BLP icons.", "icon"),
		capabilityTool("wow_creature", "Query creature displays and models.", "creature"),
		capabilityTool("wow_encounter", "Query JournalEncounter data.", "encounter"),
		capabilityTool("wow_decor", "Query decor data.", "decor"),
		capabilityTool("wow_video", "Process video container data.", "video"),
	}
}

func statusTool(provider health.Provider) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        "wow_status",
		Description: "Inspect HTTP service status.",
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			if provider == nil {
				return errorEnvelope("status", "health_unavailable", "health provider is unavailable"), nil
			}
			snapshot, err := WowStatus(ctx, provider)
			if err != nil {
				return errorEnvelope("status", "health_unavailable", err.Error()), nil
			}
			return okEnvelope("status", snapshot), nil
		},
	}
}

func queryTool(queryService service.QueryService) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        "wow_query",
		Description: "Query DB2 tables through the HTTP service.",
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			args, err := parseArgs(raw)
			if err != nil {
				return nil, err
			}
			table := stringArg(args, "table", "")
			if table == "" {
				return errorEnvelope("query", "invalid_request", "table is required"), nil
			}

			switch mode := stringArg(args, "mode", "rows"); mode {
			case "schema":
				schema, err := queryService.Schema(ctx, service.SchemaRequest{
					Context: requestContextFromArgs(args),
					Table:   table,
				})
				if err != nil {
					return errorEnvelopeFromError("query schema", "query_engine_unavailable", err), nil
				}
				return okEnvelope("query schema", map[string]interface{}{
					"table":    schema.Table,
					"mode":     "schema",
					"rowCount": schema.RowCount,
					"fields":   schema.Fields,
				}), nil
			case "", "rows":
				rows, err := queryService.Rows(ctx, service.QueryRowsRequest{
					Context: requestContextFromArgs(args),
					Table:   table,
					IDs:     uint64ListArg(args, "id", "ids"),
					IDField: stringArg(args, "field", "ID"),
					Fields:  stringListArg(args, "fields"),
					Filter:  stringArg(args, "filter", ""),
					Limit:   intArg(args, "limit", 0),
					Offset:  intArg(args, "offset", 0),
				})
				if err != nil {
					return errorEnvelopeFromError("query rows", "query_engine_unavailable", err), nil
				}
				return okEnvelope("query rows", rowsEnvelope(table, "rows", rows)), nil
			case "search":
				rows, err := queryService.Search(ctx, service.SearchRequest{
					Context: requestContextFromArgs(args),
					Table:   table,
					Field:   stringArg(args, "field", ""),
					Query:   stringArg(args, "query", ""),
					Limit:   intArg(args, "limit", 0),
				})
				if err != nil {
					return errorEnvelopeFromError("query search", "query_engine_unavailable", err), nil
				}
				return okEnvelope("query search", rowsEnvelope(table, "search", rows)), nil
			case "foreign-key":
				rows, err := queryService.ForeignKey(ctx, service.ForeignKeyRequest{
					Context: requestContextFromArgs(args),
					Table:   table,
					Field:   stringArg(args, "field", ""),
					Value:   args["value"],
					Limit:   intArg(args, "limit", 0),
				})
				if err != nil {
					return errorEnvelopeFromError("query foreign-key", "query_engine_unavailable", err), nil
				}
				return okEnvelope("query foreign-key", rowsEnvelope(table, "foreign-key", rows)), nil
			case "stream":
				rows, err := queryService.Stream(ctx, service.StreamRequest{
					Context: requestContextFromArgs(args),
					Table:   table,
					Limit:   intArg(args, "limit", 0),
					Offset:  intArg(args, "offset", 0),
				})
				if err != nil {
					return errorEnvelopeFromError("query stream", "query_engine_unavailable", err), nil
				}
				return okEnvelope("query stream", rowsEnvelope(table, "stream", rows)), nil
			default:
				return errorEnvelope("query", "invalid_mode", "mode must be schema, rows, search, foreign-key, or stream"), nil
			}
		},
	}
}

func capabilityTool(name, description, command string) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        name,
		Description: description,
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			return errorEnvelope(command, "capability_unavailable", command+" is unavailable in the HTTP service"), nil
		},
	}
}

func rowsEnvelope(table, mode string, rows []map[string]interface{}) map[string]interface{} {
	if rows == nil {
		rows = []map[string]interface{}{}
	}
	return map[string]interface{}{
		"table": table,
		"mode":  mode,
		"rows":  rows,
		"count": len(rows),
	}
}

func objectSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}

func parseArgs(raw json.RawMessage) (map[string]interface{}, error) {
	if len(raw) == 0 {
		return map[string]interface{}{}, nil
	}
	var args map[string]interface{}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	return args, nil
}

func requestContextFromArgs(args map[string]interface{}) service.RequestContext {
	return service.RequestContext{
		Region:   stringArg(args, "region", ""),
		Product:  stringArg(args, "product", ""),
		Locale:   stringArg(args, "locale", ""),
		BuildKey: stringArg(args, "buildKey", ""),
	}
}

func okEnvelope(command string, data interface{}) map[string]interface{} {
	return map[string]interface{}{
		"ok":       true,
		"command":  command,
		"data":     data,
		"warnings": []interface{}{},
	}
}

func errorEnvelope(command, code, message string) map[string]interface{} {
	return map[string]interface{}{
		"ok":       false,
		"command":  command,
		"data":     map[string]interface{}{},
		"warnings": []interface{}{},
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
}

func errorEnvelopeFromError(command, fallbackCode string, err error) map[string]interface{} {
	code := fallbackCode
	var unavailable service.CapabilityUnavailableError
	if errors.As(err, &unavailable) && unavailable.Code != "" {
		code = unavailable.Code
	}
	return errorEnvelope(command, code, err.Error())
}

func stringArg(args map[string]interface{}, key, fallback string) string {
	value, ok := args[key]
	if !ok {
		return fallback
	}
	switch v := value.(type) {
	case string:
		if v == "" {
			return fallback
		}
		return v
	case float64:
		return strconv.FormatUint(uint64(v), 10)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprint(v)
	}
}

func intArg(args map[string]interface{}, key string, fallback int) int {
	value, ok := args[key]
	if !ok {
		return fallback
	}
	switch v := value.(type) {
	case float64:
		return int(v)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return fallback
		}
		return parsed
	default:
		return fallback
	}
}

func stringListArg(args map[string]interface{}, key string) []string {
	value, ok := args[key]
	if !ok {
		return nil
	}
	switch v := value.(type) {
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if s := strings.TrimSpace(part); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func uint64ListArg(args map[string]interface{}, keys ...string) []uint64 {
	var out []uint64
	for _, key := range keys {
		value, ok := args[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case []interface{}:
			for _, item := range v {
				out = appendUint64Arg(out, fmt.Sprint(item))
			}
		case string:
			for _, part := range strings.Split(v, ",") {
				out = appendUint64Arg(out, part)
			}
		case float64:
			if v >= 0 {
				out = append(out, uint64(v))
			}
		default:
			out = appendUint64Arg(out, fmt.Sprint(v))
		}
	}
	return out
}

func appendUint64Arg(out []uint64, value string) []uint64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return out
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return out
	}
	return append(out, parsed)
}
