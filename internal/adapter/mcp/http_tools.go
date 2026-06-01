package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"

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

type BuildProvider interface {
	Builds() httpservice.BuildCatalog
}

type TableEnsurer interface {
	EnsureTable(context.Context, httpservice.RequestContext, string) error
}

type CapabilityProvider interface {
	RequireCapability(context.Context, httpservice.RequestContext, string) error
}

type HTTPService interface {
	StatusProvider
	BuildProvider
	TableEnsurer
	CapabilityProvider
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
	LinkArtifact(path, mimeType string) (ArtifactLink, error)
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
		httpCapabilityTool(svc, "wow_item", "Query item metadata and assets.", "item", "item_query"),
		httpCapabilityTool(svc, "wow_spell", "Inspect spell relationships.", "spell", "spell_query"),
		httpCapabilityTool(svc, "wow_file", "Query and export CASC files.", "file", "file_query"),
		httpIconTool(svc, opts.Artifacts),
		httpCapabilityTool(svc, "wow_creature", "Query creature displays and models.", "creature", "creature_query"),
		httpCapabilityTool(svc, "wow_encounter", "Query JournalEncounter data.", "encounter", "encounter_query"),
		httpCapabilityTool(svc, "wow_decor", "Query decor data.", "decor", "decor_query"),
		httpCapabilityTool(svc, "wow_video", "Process video container data.", "video", "video_query"),
	}
	if opts.ExposeAdmin {
		tools = append(tools,
			httpCapabilityTool(svc, "wow_refresh_builds", "Refresh known build metadata.", "refresh_builds", "refresh_builds"),
			httpCapabilityTool(svc, "wow_prepare", "Prepare cached data for a build context.", "prepare", "prepare"),
			httpCapabilityTool(svc, "wow_prune_cache", "Prune old cache artifacts.", "prune_cache", "prune_cache"),
		)
	}
	return tools
}

func httpBuildsTool(svc BuildProvider) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        "wow_builds",
		Description: "List HTTP service build contexts.",
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			return okEnvelope("builds", svc.Builds()), nil
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

func httpDB2Tool(svc interface {
	TableEnsurer
	CapabilityProvider
}) mcpserver.Tool {
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
				return errorEnvelopeFromError("db2", "materializer_unavailable", err), nil
			}
			if err := svc.RequireCapability(ctx, rc, "db2_query"); err != nil {
				return errorEnvelopeFromError("db2", "query_engine_unavailable", err), nil
			}
			return errorEnvelope("db2", "query_engine_unavailable", "DB2 query engine is unavailable in the HTTP service"), nil
		},
	}
}

func httpIconTool(svc CapabilityProvider, artifacts ArtifactReserver) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        "wow_icon",
		Description: "Export BLP icons.",
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			args, err := parseArgs(raw)
			if err != nil {
				return nil, err
			}
			artifactPath := stringArg(args, "path", "")
			mimeType := stringArg(args, "mimeType", "image/png")
			if artifactPath == "" {
				if err := svc.RequireCapability(ctx, requestContextFromArgs(args), "icon_export"); err != nil {
					return errorEnvelopeFromError("icon export", "export_engine_unavailable", err), nil
				}
				return errorEnvelope("icon export", "export_engine_unavailable", "icon export engine is unavailable in the HTTP service"), nil
			}
			if artifacts == nil {
				return errorEnvelope("icon export", "artifact_link_unavailable", "artifact manager is unavailable in the HTTP service"), nil
			}
			link, err := artifacts.LinkArtifact(artifactPath, mimeType)
			if err != nil {
				return nil, err
			}
			data := map[string]interface{}{"path": artifactPath}
			addArtifactLinkFields(data, link)
			return okEnvelope("icon export", data), nil
		},
	}
}

func httpCapabilityTool(svc CapabilityProvider, name, description, command, capability string) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        name,
		Description: description,
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			args, err := parseArgs(raw)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireCapability(ctx, requestContextFromArgs(args), capability); err != nil {
				return errorEnvelopeFromError(command, "query_engine_unavailable", err), nil
			}
			return errorEnvelope(command, "query_engine_unavailable", capability+" is unavailable in the HTTP service"), nil
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

func errorEnvelopeFromError(command, fallbackCode string, err error) map[string]interface{} {
	code := fallbackCode
	var capabilityErr httpservice.CapabilityError
	if errors.As(err, &capabilityErr) && capabilityErr.Code != "" {
		code = capabilityErr.Code
	}
	return errorEnvelope(command, code, err.Error())
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
