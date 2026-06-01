package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"wowdata/internal/export"
	"wowdata/internal/mcpserver"
	appruntime "wowdata/internal/runtime"
	httpservice "wowdata/internal/service/http"
	"wowdata/internal/wowdata"
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

type DB2Querier interface {
	QueryDB2(context.Context, httpservice.DB2Query) ([]map[string]interface{}, error)
}

type DB2SchemaQuerier interface {
	SchemaDB2(context.Context, httpservice.RequestContext, string) (httpservice.DB2Schema, error)
}

type CapabilityProvider interface {
	RequireCapability(context.Context, httpservice.RequestContext, string) error
}

type HTTPService interface {
	StatusProvider
	BuildProvider
	TableEnsurer
	DB2Querier
	DB2SchemaQuerier
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

type ArtifactAllocator interface {
	ReserveArtifact(kind, name, mimeType string) (string, ArtifactLink, error)
}

type RuntimeAssetProvider interface {
	FileStore(context.Context, httpservice.RequestContext, bool) (appruntime.FileStore, error)
	IconStore(context.Context, httpservice.RequestContext) (appruntime.IconStore, error)
}

type HTTPToolOptions struct {
	ExposeAdmin bool
	Artifacts   ArtifactReserver
	Assets      RuntimeAssetProvider
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
		httpItemTool(svc),
		httpSpellTool(svc),
		httpFileTool(svc, opts.Assets, opts.Artifacts),
		httpIconTool(svc, opts.Assets, opts.Artifacts),
		httpCreatureTool(svc),
		httpEncounterTool(svc),
		httpDecorTool(svc),
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
	DB2Querier
	DB2SchemaQuerier
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
			mode := stringArg(args, "mode", "rows")
			if mode == "schema" {
				schema, err := svc.SchemaDB2(ctx, rc, table)
				if err != nil {
					return errorEnvelopeFromError("db2 schema", "query_engine_unavailable", err), nil
				}
				return okEnvelope("db2 schema", map[string]interface{}{
					"table":    schema.Table,
					"mode":     "schema",
					"rowCount": schema.RowCount,
					"fields":   schema.Fields,
				}), nil
			}
			query := httpservice.DB2Query{
				RequestContext: rc,
				Table:          table,
				IDs:            uint32ListArg(args, "id", "ids"),
				IDField:        stringArg(args, "field", "ID"),
				Fields:         stringListArg(args, "fields"),
				Filter:         stringArg(args, "filter", ""),
				Limit:          intArg(args, "limit", 0),
			}
			switch mode {
			case "rows", "":
				mode = "rows"
			case "search":
				query.SearchField = stringArg(args, "field", "")
				query.SearchQuery = stringArg(args, "query", "")
			case "foreign-key":
				query.IDs = uint32ListArg(args, "value")
				query.IDField = stringArg(args, "field", "")
			case "stream":
			default:
				return errorEnvelope("db2", "invalid_mode", "mode must be schema, rows, search, foreign-key, or stream"), nil
			}
			rows, err := svc.QueryDB2(ctx, query)
			if err != nil {
				return errorEnvelopeFromError("db2", "query_engine_unavailable", err), nil
			}
			return okEnvelope("db2 "+mode, map[string]interface{}{
				"table": table,
				"mode":  mode,
				"rows":  rows,
				"count": len(rows),
			}), nil
		},
	}
}

func httpFileTool(svc CapabilityProvider, assets RuntimeAssetProvider, artifacts ArtifactReserver) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        "wow_file",
		Description: "Query and export CASC files.",
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			args, err := parseArgs(raw)
			if err != nil {
				return nil, err
			}
			mode := stringArg(args, "mode", "lookup")
			needsListfile := mode == "lookup" || mode == "search" || mode == "extension" || stringArg(args, "filename", "") != ""
			if assets == nil {
				if err := svc.RequireCapability(ctx, requestContextFromArgs(args), "file_query"); err != nil {
					return errorEnvelopeFromError("file "+mode, "query_engine_unavailable", err), nil
				}
				return errorEnvelope("file "+mode, "query_engine_unavailable", "file query engine is unavailable in the HTTP service"), nil
			}
			store, err := assets.FileStore(ctx, requestContextFromArgs(args), needsListfile)
			if err != nil {
				return errorEnvelopeFromError("file "+mode, "query_engine_unavailable", err), nil
			}
			fileDataID := uint32Arg(args, "fileDataID", "fileDataId", "id")
			filename := stringArg(args, "filename", "")
			switch mode {
			case "lookup":
				name, found := store.Lookup(fileDataID)
				if !found {
					name = "unknown"
				}
				return okEnvelope("file lookup", map[string]interface{}{"fileDataID": fileDataID, "fileName": name}), nil
			case "search":
				query := stringArg(args, "query", "")
				results := store.Search(query, intArg(args, "limit", 0))
				return okEnvelope("file search", map[string]interface{}{"search": query, "total": store.SearchCount(query), "returned": len(results), "files": fileEntriesEnvelope(results)}), nil
			case "extension":
				extension := stringArg(args, "extension", "")
				results := store.Extension(extension, intArg(args, "limit", 0))
				return okEnvelope("file extension", map[string]interface{}{"extension": extension, "total": store.ExtensionCount(extension), "returned": len(results), "files": formattedFileEntries(results)}), nil
			case "exists":
				exists := false
				if fileDataID != 0 {
					exists = store.ExistsByID(fileDataID)
				} else if filename != "" {
					exists = store.ExistsByName(filename)
				}
				return okEnvelope("file exists", map[string]interface{}{"fileDataID": fileDataID, "filename": filename, "exists": exists}), nil
			case "encoding":
				info, err := store.EncodingInfo(fileDataID)
				if err != nil {
					return errorEnvelope("file encoding", "not_found", err.Error()), nil
				}
				return okEnvelope("file encoding", info), nil
			case "get", "export":
				data, err := readAssetFile(store, fileDataID, filename)
				if err != nil {
					return errorEnvelope("file "+mode, "not_found", err.Error()), nil
				}
				if artifacts == nil {
					return okEnvelope("file "+mode, fileDataEnvelope(fileDataID, filename, data)), nil
				}
				path, link, err := reserveArtifact(artifacts, "files", fileArtifactName(fileDataID, filename), "application/octet-stream")
				if err != nil {
					return nil, err
				}
				if err := os.WriteFile(path, data, 0644); err != nil {
					return errorEnvelope("file "+mode, "io_error", err.Error()), nil
				}
				payload := fileDataEnvelope(fileDataID, filename, data)
				addArtifactLinkFields(payload, link)
				return okEnvelope("file "+mode, payload), nil
			default:
				return errorEnvelope("file", "invalid_mode", "mode must be lookup, search, extension, exists, encoding, get, or export"), nil
			}
		},
	}
}

func httpIconTool(svc CapabilityProvider, assets RuntimeAssetProvider, artifacts ArtifactReserver) mcpserver.Tool {
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
				if assets == nil || artifacts == nil {
					if err := svc.RequireCapability(ctx, requestContextFromArgs(args), "icon_export"); err != nil {
						return errorEnvelopeFromError("icon export", "export_engine_unavailable", err), nil
					}
					return errorEnvelope("icon export", "export_engine_unavailable", "icon export engine is unavailable in the HTTP service"), nil
				}
				return exportHTTPIcon(ctx, assets, artifacts, requestContextFromArgs(args), args)
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

type BusinessQuerier interface {
	TableEnsurer
	DB2Querier
}

func httpItemTool(svc BusinessQuerier) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        "wow_item",
		Description: "Query item metadata and assets.",
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			args, err := parseArgs(raw)
			if err != nil {
				return nil, err
			}
			rc := requestContextFromArgs(args)
			if err := ensureBusinessTables(ctx, svc, rc, "Item", "ItemSparse", "ItemEffect", "ItemModifiedAppearance", "ItemAppearance", "ItemDisplayInfo", "ItemDisplayInfoMaterialRes", "ModelFileData", "TextureFileData", "ComponentModelFileData", "HelmetGeosetData"); err != nil {
				return errorEnvelopeFromError("item "+stringArg(args, "mode", "get"), "query_engine_unavailable", err), nil
			}
			store := httpRowStore{ctx: ctx, rc: rc, svc: svc}
			items := wowdata.NewItemServiceWithDB2(store)
			mode := stringArg(args, "mode", "get")
			itemID := uint32Arg(args, "itemID", "itemId", "id")
			switch mode {
			case "get":
				item := items.GetItem(itemID)
				if item == nil {
					return errorEnvelope("item get", "not_found", "item not found"), nil
				}
				return okEnvelope("item get", item), nil
			case "models":
				result := items.GetItemModels(int(itemID), intArg(args, "raceID", 0), intArg(args, "gender", 0))
				return okEnvelope("item models", map[string]interface{}{
					"itemID":  itemID,
					"raceID":  intArg(args, "raceID", 0),
					"gender":  intArg(args, "gender", 0),
					"display": itemModelDisplay(result),
				}), nil
			case "geosets":
				result := items.GetItemGeosets(itemID)
				return okEnvelope("item geosets", itemGeosetsEnvelope(itemID, result)), nil
			case "textures":
				result := items.GetItemTextures(itemID)
				return okEnvelope("item textures", map[string]interface{}{"itemID": itemID, "textures": itemTexturesEnvelope(result)}), nil
			default:
				return errorEnvelope("item", "invalid_mode", "mode must be get, models, geosets, or textures"), nil
			}
		},
	}
}

func httpSpellTool(svc BusinessQuerier) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        "wow_spell",
		Description: "Inspect spell relationships.",
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			args, err := parseArgs(raw)
			if err != nil {
				return nil, err
			}
			rc := requestContextFromArgs(args)
			if err := ensureBusinessTables(ctx, svc, rc, "SpellName", "Spell", "SpellEffect", "SpellMisc", "SpellCastTimes", "SpellDuration", "SpellRange"); err != nil {
				return errorEnvelopeFromError("spell "+stringArg(args, "mode", "info"), "query_engine_unavailable", err), nil
			}
			spells := wowdata.NewSpellServiceWithDB2(httpRowStore{ctx: ctx, rc: rc, svc: svc})
			spellID := uint32Arg(args, "spellID", "spellId", "id")
			switch mode := stringArg(args, "mode", "info"); mode {
			case "info":
				return okEnvelope("spell info", spells.GetSpellInfo(spellID, intArg(args, "maxDepth", 5))), nil
			case "auras":
				data := map[string]interface{}{"hasAura": []uint32{}, "noAura": []uint32{}}
				if spells.DetectAuras(spellID).HasAura {
					data["hasAura"] = []uint32{spellID}
				} else {
					data["noAura"] = []uint32{spellID}
				}
				return okEnvelope("spell auras", data), nil
			case "summons":
				summons := spells.DetectSummons(spellID, uint32Arg(args, "npcID", "npcId"))
				return okEnvelope("spell summons", map[string]interface{}{"spellID": spellID, "npcID": uint32Arg(args, "npcID", "npcId"), "summons": summons, "count": len(summons)}), nil
			default:
				return errorEnvelope("spell", "invalid_mode", "mode must be info, auras, or summons"), nil
			}
		},
	}
}

func httpEncounterTool(svc BusinessQuerier) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        "wow_encounter",
		Description: "Query JournalEncounter data.",
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			args, err := parseArgs(raw)
			if err != nil {
				return nil, err
			}
			rc := requestContextFromArgs(args)
			if err := ensureBusinessTables(ctx, svc, rc, "JournalEncounterSection", "SpellName"); err != nil {
				return errorEnvelopeFromError("encounter get", "query_engine_unavailable", err), nil
			}
			encounters := wowdata.NewEncounterServiceWithDB2(httpRowStore{ctx: ctx, rc: rc, svc: svc})
			return okEnvelope("encounter get", encounters.GetEncounter(uint32Arg(args, "journalEncounterID", "journalEncounterId", "id"))), nil
		},
	}
}

func httpCreatureTool(svc BusinessQuerier) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        "wow_creature",
		Description: "Query creature displays and models.",
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			args, err := parseArgs(raw)
			if err != nil {
				return nil, err
			}
			rc := requestContextFromArgs(args)
			if err := ensureBusinessTables(ctx, svc, rc, "CreatureDisplayInfo", "CreatureModelData", "CreatureDisplayInfoGeosetData"); err != nil {
				return errorEnvelopeFromError("creature "+stringArg(args, "mode", "display"), "query_engine_unavailable", err), nil
			}
			creatures := wowdata.NewCreatureServiceWithDB2(httpRowStore{ctx: ctx, rc: rc, svc: svc})
			displayID := uint32Arg(args, "displayID", "displayId", "id")
			fileDataID := uint32Arg(args, "fileDataID", "fileDataId")
			switch mode := stringArg(args, "mode", "display"); mode {
			case "display":
				var display *wowdata.CreatureDisplayInfo
				if displayID != 0 {
					display = creatures.GetDisplayByID(displayID)
				} else {
					display = creatures.GetDisplayByFileDataID(fileDataID)
				}
				if display == nil {
					return errorEnvelope("creature display", "not_found", "creature display not found"), nil
				}
				return okEnvelope("creature display", display), nil
			case "model":
				displays := creatures.GetCreatureDisplaysByFileDataID(fileDataID)
				return okEnvelope("creature model", creatureDisplaysEnvelope(displays)), nil
			default:
				return errorEnvelope("creature", "invalid_mode", "mode must be display or model"), nil
			}
		},
	}
}

func httpDecorTool(svc BusinessQuerier) mcpserver.Tool {
	return mcpserver.Tool{
		Name:        "wow_decor",
		Description: "Query decor data.",
		InputSchema: objectSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
			args, err := parseArgs(raw)
			if err != nil {
				return nil, err
			}
			rc := requestContextFromArgs(args)
			if err := ensureBusinessTables(ctx, svc, rc, "HouseDecor"); err != nil {
				return errorEnvelopeFromError("decor "+stringArg(args, "mode", "list"), "query_engine_unavailable", err), nil
			}
			decor := wowdata.NewDecorServiceWithDB2(httpRowStore{ctx: ctx, rc: rc, svc: svc})
			switch mode := stringArg(args, "mode", "list"); mode {
			case "get":
				item := decor.GetByID(uint32Arg(args, "id"))
				if item == nil {
					return errorEnvelope("decor get", "not_found", "decor item not found"), nil
				}
				return okEnvelope("decor get", item), nil
			case "model":
				item := decor.GetByModelFileDataID(uint32Arg(args, "modelFileDataID", "modelFileDataId", "fileDataID", "fileDataId"))
				if item == nil {
					return errorEnvelope("decor model", "not_found", "decor item not found"), nil
				}
				return okEnvelope("decor model", item), nil
			case "list":
				items := decor.ListAll()
				if limit := intArg(args, "limit", 0); limit > 0 && limit < len(items) {
					items = items[:limit]
				}
				return okEnvelope("decor list", map[string]interface{}{"items": items, "count": len(items)}), nil
			default:
				return errorEnvelope("decor", "invalid_mode", "mode must be list, get, or model"), nil
			}
		},
	}
}

func ensureBusinessTables(ctx context.Context, svc TableEnsurer, rc httpservice.RequestContext, tables ...string) error {
	for _, table := range tables {
		if err := svc.EnsureTable(ctx, rc, table); err != nil {
			return err
		}
	}
	return nil
}

type httpRowStore struct {
	ctx context.Context
	rc  httpservice.RequestContext
	svc DB2Querier
}

func (s httpRowStore) Ready() bool {
	return s.svc != nil
}

func (s httpRowStore) Rows(table string, ids []uint32, fields []string, filter string, limit int) ([]map[string]interface{}, error) {
	if s.svc == nil {
		return nil, httpservice.NewCapabilityError("query_engine_unavailable", "query engine")
	}
	return s.svc.QueryDB2(s.ctx, httpservice.DB2Query{
		RequestContext: s.rc,
		Table:          table,
		IDs:            ids,
		Fields:         fields,
		Filter:         filter,
		Limit:          limit,
	})
}

func itemModelDisplay(result *wowdata.ItemModelResult) map[string]interface{} {
	if result == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"ID":                    result.DisplayID,
		"models":                result.Models,
		"textures":              result.Textures,
		"geosetGroup":           result.GeosetGroup,
		"attachmentGeosetGroup": []int{0, 0, 0, 0, 0, 0},
	}
}

func itemGeosetsEnvelope(itemID uint32, result *wowdata.ItemGeosetResult) map[string]interface{} {
	helmetHide := []int{}
	geosets := map[string]interface{}{"geosetGroup": []int{}, "helmetGeosetVis": []int{}}
	if result != nil {
		helmetHide = append(helmetHide, result.HelmetHide...)
		geosets = map[string]interface{}{"geosetGroup": result.GeosetGroup, "helmetGeosetVis": result.HelmetGeosetVis}
	}
	return map[string]interface{}{"itemID": itemID, "geosets": geosets, "helmetHide": helmetHide}
}

func itemTexturesEnvelope(result *wowdata.ItemTextureResult) []map[string]interface{} {
	if result == nil {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(result.Sections))
	for _, section := range result.Sections {
		out = append(out, map[string]interface{}{"section": section.Section, "fileDataID": section.FileDataID})
	}
	return out
}

func creatureDisplaysEnvelope(displays []wowdata.CreatureDisplayInfo) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(displays))
	for _, display := range displays {
		textures := display.Textures
		if textures == nil {
			textures = []uint32{}
		}
		out = append(out, map[string]interface{}{
			"ID":       display.DisplayID,
			"modelID":  display.ModelID,
			"textures": textures,
		})
	}
	return out
}

func exportHTTPIcon(ctx context.Context, assets RuntimeAssetProvider, artifacts ArtifactReserver, rc httpservice.RequestContext, args map[string]interface{}) (interface{}, error) {
	store, err := assets.IconStore(ctx, rc)
	if err != nil {
		return errorEnvelopeFromError("icon export", "export_engine_unavailable", err), nil
	}
	fileDataID := uint32Arg(args, "fileDataID", "fileDataId", "id")
	format := strings.ToLower(strings.TrimSpace(stringArg(args, "format", "png")))
	if format == "" {
		format = "png"
	}
	if format != "png" && format != "webp" {
		return errorEnvelope("icon export", "unsupported_format", "format must be png or webp"), nil
	}
	mimeType := "image/" + format
	path, link, err := reserveArtifact(artifacts, "icons", fmt.Sprintf("%d.%s", fileDataID, format), mimeType)
	if err != nil {
		return nil, err
	}
	data, err := store.ReadByID(fileDataID)
	if err != nil {
		return errorEnvelope("icon export", "not_found", err.Error()), nil
	}
	result, err := export.ExportIconWithOptions(data, path, format, intArg(args, "mipmap", 0), intArg(args, "mask", 0))
	if err != nil {
		return errorEnvelope("icon export", "export_error", err.Error()), nil
	}
	payload := map[string]interface{}{"fileDataID": fileDataID, "format": format, "result": result}
	addArtifactLinkFields(payload, link)
	return okEnvelope("icon export", payload), nil
}

func reserveArtifact(artifacts ArtifactReserver, kind, name, mimeType string) (string, ArtifactLink, error) {
	if allocator, ok := artifacts.(ArtifactAllocator); ok {
		return allocator.ReserveArtifact(kind, name, mimeType)
	}
	return "", ArtifactLink{}, fmt.Errorf("artifact reserve is unavailable in the HTTP service")
}

func readAssetFile(store appruntime.FileStore, fileDataID uint32, filename string) ([]byte, error) {
	if fileDataID != 0 {
		return store.ReadByID(fileDataID)
	}
	return store.ReadByName(filename)
}

func fileDataEnvelope(fileDataID uint32, filename string, data []byte) map[string]interface{} {
	sum := sha256.Sum256(data)
	return map[string]interface{}{
		"fileDataID": fileDataID,
		"filename":   filename,
		"size":       len(data),
		"sha256":     fmt.Sprintf("%x", sum[:]),
	}
}

func fileArtifactName(fileDataID uint32, filename string) string {
	if filename != "" {
		return filepath.Base(filepath.ToSlash(filename))
	}
	if fileDataID != 0 {
		return fmt.Sprintf("%d.bin", fileDataID)
	}
	return "file.bin"
}

func fileEntriesEnvelope(entries []appruntime.FileEntry) []map[string]interface{} {
	files := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		files = append(files, map[string]interface{}{"fileDataID": entry.FileDataID, "fileName": entry.Filename})
	}
	return files
}

func formattedFileEntries(entries []appruntime.FileEntry) []string {
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		files = append(files, fmt.Sprintf("%s [%d]", entry.Filename, entry.FileDataID))
	}
	return files
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

func uint32Arg(args map[string]interface{}, keys ...string) uint32 {
	for _, key := range keys {
		value, ok := args[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case float64:
			if v >= 0 {
				return uint32(v)
			}
		case string:
			parsed, err := strconv.ParseUint(strings.TrimSpace(v), 10, 32)
			if err == nil {
				return uint32(parsed)
			}
		default:
			parsed, err := strconv.ParseUint(strings.TrimSpace(fmt.Sprint(v)), 10, 32)
			if err == nil {
				return uint32(parsed)
			}
		}
	}
	return 0
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

func uint32ListArg(args map[string]interface{}, keys ...string) []uint32 {
	var out []uint32
	for _, key := range keys {
		value, ok := args[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case []interface{}:
			for _, item := range v {
				out = appendUint32Arg(out, fmt.Sprint(item))
			}
		case string:
			for _, part := range strings.Split(v, ",") {
				out = appendUint32Arg(out, part)
			}
		case float64:
			if v >= 0 {
				out = append(out, uint32(v))
			}
		default:
			out = appendUint32Arg(out, fmt.Sprint(v))
		}
	}
	return out
}

func appendUint32Arg(out []uint32, value string) []uint32 {
	value = strings.TrimSpace(value)
	if value == "" {
		return out
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return out
	}
	return append(out, uint32(parsed))
}
