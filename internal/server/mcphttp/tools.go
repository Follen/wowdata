package mcphttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
		buildsTool(opts.HealthProvider),
		queryTool(queryService),
		itemTool(queryService),
		spellTool(queryService),
		capabilityTool("wow_file", "Query and export CASC files.", "file"),
		capabilityTool("wow_icon", "Export BLP icons.", "icon"),
		creatureTool(queryService),
		encounterTool(queryService),
		decorTool(queryService),
		capabilityTool("wow_video", "Process video container data.", "video"),
	}
}

func buildsTool(provider health.Provider) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        "wow_builds",
		Description: "List prepared build contexts.",
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			if provider == nil {
				return errorEnvelope("builds", "health_unavailable", "health provider is unavailable"), nil
			}
			snapshot, err := provider.HealthSnapshot(ctx)
			if err != nil {
				return errorEnvelope("builds", "health_unavailable", err.Error()), nil
			}
			return okEnvelope("builds", map[string]interface{}{
				"contexts": snapshot.Contexts,
				"count":    len(snapshot.Contexts),
			}), nil
		},
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

			switch mode := stringArg(args, "mode", "rows"); mode {
			case "tables":
				catalog, err := queryService.Tables(ctx, service.TablesRequest{
					Context: requestContextFromArgs(args),
				})
				if err != nil {
					return errorEnvelopeFromError("query tables", "query_engine_unavailable", err), nil
				}
				return okEnvelope("query tables", tablesEnvelope(catalog.Tables)), nil
			case "schema":
				table := stringArg(args, "table", "")
				if table == "" {
					return errorEnvelope("query", "invalid_request", "table is required"), nil
				}
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
				table := stringArg(args, "table", "")
				if table == "" {
					return errorEnvelope("query", "invalid_request", "table is required"), nil
				}
				ids, err := uint64ListArg(args, "id", "ids")
				if err != nil {
					return errorEnvelope("query rows", "invalid_request", err.Error()), nil
				}
				limit, err := nonNegativeIntArg(args, "limit", 0)
				if err != nil {
					return errorEnvelope("query rows", "invalid_request", err.Error()), nil
				}
				offset, err := nonNegativeIntArg(args, "offset", 0)
				if err != nil {
					return errorEnvelope("query rows", "invalid_request", err.Error()), nil
				}
				rows, err := queryService.Rows(ctx, service.QueryRowsRequest{
					Context: requestContextFromArgs(args),
					Table:   table,
					IDs:     ids,
					IDField: stringArg(args, "field", "ID"),
					Fields:  stringListArg(args, "fields"),
					Filter:  stringArg(args, "filter", ""),
					Limit:   limit,
					Offset:  offset,
				})
				if err != nil {
					return errorEnvelopeFromError("query rows", "query_engine_unavailable", err), nil
				}
				return okEnvelope("query rows", rowsEnvelope(table, "rows", rows)), nil
			case "search":
				table := stringArg(args, "table", "")
				if table == "" {
					return errorEnvelope("query", "invalid_request", "table is required"), nil
				}
				limit, err := nonNegativeIntArg(args, "limit", 0)
				if err != nil {
					return errorEnvelope("query search", "invalid_request", err.Error()), nil
				}
				rows, err := queryService.Search(ctx, service.SearchRequest{
					Context: requestContextFromArgs(args),
					Table:   table,
					Field:   stringArg(args, "field", ""),
					Query:   stringArg(args, "query", ""),
					Limit:   limit,
				})
				if err != nil {
					return errorEnvelopeFromError("query search", "query_engine_unavailable", err), nil
				}
				return okEnvelope("query search", rowsEnvelope(table, "search", rows)), nil
			case "foreign-key":
				table := stringArg(args, "table", "")
				if table == "" {
					return errorEnvelope("query", "invalid_request", "table is required"), nil
				}
				limit, err := nonNegativeIntArg(args, "limit", 0)
				if err != nil {
					return errorEnvelope("query foreign-key", "invalid_request", err.Error()), nil
				}
				rows, err := queryService.ForeignKey(ctx, service.ForeignKeyRequest{
					Context: requestContextFromArgs(args),
					Table:   table,
					Field:   stringArg(args, "field", ""),
					Value:   args["value"],
					Limit:   limit,
				})
				if err != nil {
					return errorEnvelopeFromError("query foreign-key", "query_engine_unavailable", err), nil
				}
				return okEnvelope("query foreign-key", rowsEnvelope(table, "foreign-key", rows)), nil
			case "stream":
				table := stringArg(args, "table", "")
				if table == "" {
					return errorEnvelope("query", "invalid_request", "table is required"), nil
				}
				limit, err := nonNegativeIntArg(args, "limit", 0)
				if err != nil {
					return errorEnvelope("query stream", "invalid_request", err.Error()), nil
				}
				offset, err := nonNegativeIntArg(args, "offset", 0)
				if err != nil {
					return errorEnvelope("query stream", "invalid_request", err.Error()), nil
				}
				rows, err := queryService.Stream(ctx, service.StreamRequest{
					Context: requestContextFromArgs(args),
					Table:   table,
					Limit:   limit,
					Offset:  offset,
				})
				if err != nil {
					return errorEnvelopeFromError("query stream", "query_engine_unavailable", err), nil
				}
				return okEnvelope("query stream", rowsEnvelope(table, "stream", rows)), nil
			default:
				return errorEnvelope("query", "invalid_mode", "mode must be schema, rows, search, foreign-key, stream, or tables"), nil
			}
		},
	}
}

func itemTool(queryService service.QueryService) mcpserver.Tool {
	return businessTool("wow_item", "Query item metadata and assets.", "item", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		itemID, err := requiredUint32Arg(args, "itemID", "id")
		if err != nil {
			return nil, err
		}
		return service.ItemInfo(ctx, queryService, service.ItemInfoRequest{
			Context: requestContextFromArgs(args),
			ItemID:  itemID,
		})
	})
}

func spellTool(queryService service.QueryService) mcpserver.Tool {
	return businessTool("wow_spell", "Inspect spell relationships.", "spell", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		spellIDs, err := requiredUint32ListArg(args, "spellID", "spellIDs")
		if err != nil {
			return nil, err
		}
		maxDepth, err := nonNegativeIntArg(args, "maxDepth", 5)
		if err != nil {
			return nil, invalidRequest(err)
		}
		return service.SpellInfo(ctx, queryService, service.SpellInfoRequest{
			Context:  requestContextFromArgs(args),
			SpellIDs: spellIDs,
			MaxDepth: maxDepth,
		})
	})
}

func creatureTool(queryService service.QueryService) mcpserver.Tool {
	return businessTool("wow_creature", "Query creature displays and models.", "creature", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		displayID, hasDisplayID, err := optionalUint32Arg(args, "displayID")
		if err != nil {
			return nil, err
		}
		fileDataID, hasFileDataID, err := optionalUint32Arg(args, "fileDataID")
		if err != nil {
			return nil, err
		}
		if !hasDisplayID && !hasFileDataID {
			return nil, invalidRequest(fmt.Errorf("displayID or fileDataID is required"))
		}
		return service.CreatureDisplay(ctx, queryService, service.CreatureDisplayRequest{
			Context:    requestContextFromArgs(args),
			DisplayID:  displayID,
			FileDataID: fileDataID,
		})
	})
}

func encounterTool(queryService service.QueryService) mcpserver.Tool {
	return businessTool("wow_encounter", "Query JournalEncounter data.", "encounter", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		encounterID, err := requiredUint32Arg(args, "journalEncounterID", "encounterID")
		if err != nil {
			return nil, err
		}
		return service.EncounterInfo(ctx, queryService, service.EncounterInfoRequest{
			Context:            requestContextFromArgs(args),
			JournalEncounterID: encounterID,
		})
	})
}

func decorTool(queryService service.QueryService) mcpserver.Tool {
	return businessTool("wow_decor", "Query decor data.", "decor", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		id, hasID, err := optionalUint32Arg(args, "id")
		if err != nil {
			return nil, err
		}
		modelFileDataID, hasModelFileDataID, err := optionalUint32Arg(args, "modelFileDataID")
		if err != nil {
			return nil, err
		}
		if !hasID && !hasModelFileDataID {
			return nil, invalidRequest(fmt.Errorf("id or modelFileDataID is required"))
		}
		return service.DecorItem(ctx, queryService, service.DecorItemRequest{
			Context:         requestContextFromArgs(args),
			ID:              id,
			ModelFileDataID: modelFileDataID,
		})
	})
}

func businessTool(name, description, command string, run func(context.Context, map[string]interface{}) (interface{}, error)) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        name,
		Description: description,
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			args, err := parseArgs(raw)
			if err != nil {
				return nil, err
			}
			result, err := run(ctx, args)
			if err != nil {
				var invalid invalidRequestError
				if errors.As(err, &invalid) {
					return errorEnvelope(command, "invalid_request", invalid.Error()), nil
				}
				return errorEnvelopeFromError(command, "query_engine_unavailable", err), nil
			}
			return okEnvelope(command, map[string]interface{}{"result": result}), nil
		},
	}
}

type invalidRequestError struct {
	err error
}

func (e invalidRequestError) Error() string {
	return e.err.Error()
}

func (e invalidRequestError) Unwrap() error {
	return e.err
}

func invalidRequest(err error) error {
	if err == nil {
		return nil
	}
	return invalidRequestError{err: err}
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

func tablesEnvelope(tables []service.TableInfo) map[string]interface{} {
	if tables == nil {
		tables = []service.TableInfo{}
	}
	return map[string]interface{}{
		"mode":   "tables",
		"tables": tables,
		"count":  len(tables),
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

func nonNegativeIntArg(args map[string]interface{}, key string, fallback int) (int, error) {
	value, ok := args[key]
	if !ok {
		return fallback, nil
	}
	switch v := value.(type) {
	case float64:
		if v < 0 || math.Trunc(v) != v {
			return 0, fmt.Errorf("%s must be a non-negative integer", key)
		}
		return int(v), nil
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || parsed < 0 {
			return 0, fmt.Errorf("%s must be a non-negative integer", key)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("%s must be a non-negative integer", key)
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

func uint64ListArg(args map[string]interface{}, keys ...string) ([]uint64, error) {
	var out []uint64
	for _, key := range keys {
		value, ok := args[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case []interface{}:
			for _, item := range v {
				parsed, err := parseUint64Value(item, key)
				if err != nil {
					return nil, err
				}
				out = append(out, parsed)
			}
		case string:
			for _, part := range strings.Split(v, ",") {
				parsed, err := parseUint64String(part, key)
				if err != nil {
					return nil, err
				}
				out = append(out, parsed)
			}
		case float64:
			parsed, err := parseUint64Float(v, key)
			if err != nil {
				return nil, err
			}
			out = append(out, parsed)
		default:
			parsed, err := parseUint64Value(v, key)
			if err != nil {
				return nil, err
			}
			out = append(out, parsed)
		}
	}
	return out, nil
}

func optionalUint32Arg(args map[string]interface{}, key string) (uint32, bool, error) {
	value, ok := args[key]
	if !ok {
		return 0, false, nil
	}
	parsed, err := parseUint64Value(value, key)
	if err != nil {
		return 0, true, invalidRequest(err)
	}
	if parsed > math.MaxUint32 {
		return 0, true, invalidRequest(fmt.Errorf("%s must fit uint32", key))
	}
	return uint32(parsed), true, nil
}

func requiredUint32Arg(args map[string]interface{}, keys ...string) (uint32, error) {
	for _, key := range keys {
		parsed, ok, err := optionalUint32Arg(args, key)
		if err != nil {
			return 0, err
		}
		if !ok {
			continue
		}
		return parsed, nil
	}
	return 0, invalidRequest(fmt.Errorf("%s is required", strings.Join(keys, " or ")))
}

func uint32ListArg(args map[string]interface{}, keys ...string) ([]uint32, error) {
	values, err := uint64ListArg(args, keys...)
	if err != nil {
		return nil, invalidRequest(err)
	}
	out := make([]uint32, 0, len(values))
	for _, value := range values {
		if value > math.MaxUint32 {
			return nil, invalidRequest(fmt.Errorf("%s must fit uint32", strings.Join(keys, "/")))
		}
		out = append(out, uint32(value))
	}
	return out, nil
}

func requiredUint32ListArg(args map[string]interface{}, keys ...string) ([]uint32, error) {
	values, err := uint32ListArg(args, keys...)
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, invalidRequest(fmt.Errorf("%s is required", strings.Join(keys, " or ")))
	}
	return values, nil
}

func parseUint64Value(value interface{}, key string) (uint64, error) {
	switch v := value.(type) {
	case float64:
		return parseUint64Float(v, key)
	case string:
		return parseUint64String(v, key)
	default:
		return parseUint64String(fmt.Sprint(v), key)
	}
}

func parseUint64Float(value float64, key string) (uint64, error) {
	if value < 0 || math.Trunc(value) != value {
		return 0, fmt.Errorf("%s must contain non-negative integer IDs", key)
	}
	return uint64(value), nil
}

func parseUint64String(value string, key string) (uint64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("%s must contain non-negative integer IDs", key)
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must contain non-negative integer IDs", key)
	}
	return parsed, nil
}
