package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"wowdata/internal/mcpserver"
	httpservice "wowdata/internal/service/http"
)

var httpDefaultToolNames = []string{
	"wow_builds",
	"wow_status",
	"wow_db2",
	"wow_item",
	"wow_spell",
	"wow_file",
	"wow_icon",
	"wow_creature",
	"wow_encounter",
	"wow_decor",
	"wow_video",
}

var httpAdminToolNames = []string{
	"wow_refresh_builds",
	"wow_prepare",
	"wow_prune_cache",
}

type StatusProvider interface {
	Status() httpservice.Status
}

type TableEnsurer interface {
	EnsureTable(context.Context, httpservice.RequestContext, string) error
}

type HTTPService interface {
	StatusProvider
	TableEnsurer
}

type ArtifactLink struct {
	Path        string
	URI         string
	DownloadURL string
	MimeType    string
	Name        string
	Size        int64
	SHA256      string
}

type ArtifactReserver interface {
	ReserveArtifact(category, filename, mimeType string) (string, ArtifactLink, error)
}

type HTTPToolOptions struct {
	ExposeAdmin bool
	Artifacts   ArtifactReserver
}

func HTTPToolNames(exposeAdmin bool) []string {
	names := append([]string{}, httpDefaultToolNames...)
	if exposeAdmin {
		names = append(names, httpAdminToolNames...)
	}
	return names
}

func HTTPTools(svc HTTPService, opts HTTPToolOptions) []mcpserver.Tool {
	tools := []mcpserver.Tool{
		httpBuildsTool(svc),
		httpStatusTool(svc),
		httpDB2Tool(svc),
		httpPlaceholderTool("wow_item", "Query item metadata and assets.", "item"),
		httpPlaceholderTool("wow_spell", "Inspect spell relationships.", "spell"),
		httpPlaceholderTool("wow_file", "Query and export CASC files.", "file"),
		httpIconTool(opts.Artifacts),
		httpPlaceholderTool("wow_creature", "Query creature displays and models.", "creature"),
		httpPlaceholderTool("wow_encounter", "Query JournalEncounter data.", "encounter"),
		httpPlaceholderTool("wow_decor", "Query decor data.", "decor"),
		httpPlaceholderTool("wow_video", "Process video container data.", "video"),
	}
	if opts.ExposeAdmin {
		tools = append(tools,
			httpPlaceholderTool("wow_refresh_builds", "Refresh known build metadata.", "refresh_builds"),
			httpPlaceholderTool("wow_prepare", "Prepare cached data for a build context.", "prepare"),
			httpPlaceholderTool("wow_prune_cache", "Prune old cache artifacts.", "prune_cache"),
		)
	}
	return tools
}

func httpBuildsTool(svc StatusProvider) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        "wow_builds",
		Description: "List HTTP service build contexts.",
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			status := svc.Status()
			return okEnvelope("builds", status.Contexts), nil
		},
	}
}

func httpStatusTool(svc StatusProvider) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        "wow_status",
		Description: "Inspect HTTP service status.",
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			return okEnvelope("status", svc.Status()), nil
		},
	}
}

func httpDB2Tool(svc TableEnsurer) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        "wow_db2",
		Description: "Query DB2 tables through the HTTP service.",
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			args, err := parseArgs(raw)
			if err != nil {
				return nil, err
			}
			table := stringArg(args, "table", "")
			if table == "" {
				return errorEnvelope("db2", "invalid_request", "table is required"), nil
			}
			rc := requestContextFromArgs(args)
			if err := svc.EnsureTable(ctx, rc, table); err != nil {
				return errorEnvelope("db2", "materializer_unavailable", err.Error()), nil
			}
			return okEnvelope("db2", map[string]interface{}{
				"table":  table,
				"status": "query placeholder unavailable",
			}), nil
		},
	}
}

func httpIconTool(artifacts ArtifactReserver) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        "wow_icon",
		Description: "Export BLP icons.",
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			args, err := parseArgs(raw)
			if err != nil {
				return nil, err
			}
			fileDataID := stringArg(args, "fileDataID", "")
			if fileDataID == "" {
				return errorEnvelope("icon export", "invalid_request", "fileDataID is required"), nil
			}
			format := strings.TrimPrefix(stringArg(args, "format", "png"), ".")
			mimeType := "image/" + format
			filename := fileDataID + "." + format
			data := map[string]interface{}{
				"fileDataID": fileDataID,
				"status":     "export placeholder unavailable",
			}
			if artifacts != nil {
				path, link, err := artifacts.ReserveArtifact("icons", filename, mimeType)
				if err != nil {
					return nil, err
				}
				data["path"] = path
				addArtifactLinkFields(data, link)
			}
			return okEnvelope("icon export", data), nil
		},
	}
}

func httpPlaceholderTool(name, description, command string) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        name,
		Description: description,
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			return errorEnvelope(command, "query_unavailable", "HTTP MCP handler is not implemented yet"), nil
		},
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

func requestContextFromArgs(args map[string]interface{}) httpservice.RequestContext {
	return httpservice.RequestContext{
		Region:  stringArg(args, "region", ""),
		Product: stringArg(args, "product", ""),
		Locale:  stringArg(args, "locale", ""),
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

func addArtifactLinkFields(data map[string]interface{}, link ArtifactLink) {
	if link.Path != "" {
		data["path"] = link.Path
	}
	if link.URI != "" {
		data["uri"] = link.URI
	}
	if link.DownloadURL != "" {
		data["downloadUrl"] = link.DownloadURL
	}
	if link.MimeType != "" {
		data["mimeType"] = link.MimeType
	}
	if link.Name != "" {
		data["name"] = link.Name
	} else if link.Path != "" {
		data["name"] = filepath.Base(link.Path)
	}
	if link.Size != 0 {
		data["size"] = link.Size
	}
	if link.SHA256 != "" {
		data["sha256"] = link.SHA256
	}
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
