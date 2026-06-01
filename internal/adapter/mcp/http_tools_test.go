package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"wowdata/internal/mcpserver"
	appruntime "wowdata/internal/runtime"
	httpservice "wowdata/internal/service/http"
)

func TestHTTPToolNamesDefaultExcludeWarmupAndIncludeHTTPTools(t *testing.T) {
	names := stringSet(HTTPToolNames(false))

	if names["wow_warmup"] {
		t.Fatalf("HTTPToolNames(false) should not include wow_warmup; got %#v", names)
	}
	for _, want := range []string{
		"wow_builds",
		"wow_status",
		"wow_query",
		"wow_item",
		"wow_spell",
		"wow_file",
		"wow_icon",
		"wow_creature",
		"wow_encounter",
		"wow_decor",
		"wow_video",
	} {
		if !names[want] {
			t.Fatalf("HTTPToolNames(false) missing %q; got %#v", want, names)
		}
	}
	legacyName := "wow_" + "db2"
	if names[legacyName] {
		t.Fatalf("HTTPToolNames(false) should not expose %s; got %#v", legacyName, names)
	}
}

func TestHTTPToolNamesAdminIncludeAdminTools(t *testing.T) {
	names := stringSet(HTTPToolNames(true))

	for _, want := range []string{"wow_refresh_builds", "wow_prepare", "wow_prune_cache"} {
		if !names[want] {
			t.Fatalf("HTTPToolNames(true) missing admin tool %q; got %#v", want, names)
		}
	}
}

func TestHTTPStatusHandlerReturnsServiceStatusWithoutWarmup(t *testing.T) {
	svc := &fakeHTTPService{
		status: httpservice.Status{
			OK: true,
			Cache: httpservice.CacheStatus{
				Root: "cache-root",
			},
		},
	}
	tool := findTool(t, HTTPTools(svc, HTTPToolOptions{}), "wow_status")

	result, err := tool.Handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("wow_status handler: %v", err)
	}
	if svc.statusCalls != 1 {
		t.Fatalf("Status calls = %d, want 1", svc.statusCalls)
	}
	got := result.(map[string]interface{})
	if got["command"] != "status" || got["ok"] != true {
		t.Fatalf("unexpected status envelope: %#v", got)
	}
	data := got["data"].(httpservice.Status)
	if data.Cache.Root != "cache-root" {
		t.Fatalf("status cache root = %q", data.Cache.Root)
	}
}

func TestHTTPBuildsHandlerReturnsDefaultsAndPinnedContexts(t *testing.T) {
	svc := &fakeHTTPService{
		builds: httpservice.BuildCatalog{
			Default: httpservice.ContextDefaults{Region: "cn", Product: "wow", Locale: "zhCN"},
			Pinned: []httpservice.PinnedContext{
				{Region: "cn", Product: "wowt", Locale: "zhCN", Label: "PTR"},
			},
		},
	}
	tool := findTool(t, HTTPTools(svc, HTTPToolOptions{}), "wow_builds")

	result, err := tool.Handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("wow_builds handler: %v", err)
	}
	if svc.buildCalls != 1 {
		t.Fatalf("Builds calls = %d, want 1", svc.buildCalls)
	}
	data := result.(map[string]interface{})["data"].(httpservice.BuildCatalog)
	if data.Default.Region != "cn" || data.Default.Product != "wow" || data.Default.Locale != "zhCN" {
		t.Fatalf("defaults mismatch: %#v", data.Default)
	}
	if len(data.Pinned) != 1 || data.Pinned[0].Label != "PTR" {
		t.Fatalf("pinned contexts mismatch: %#v", data.Pinned)
	}
}

func TestHTTPDB2HandlerInvokesEnsureTableAndReturnsMaterializerError(t *testing.T) {
	svc := &fakeHTTPService{
		ensureErr: errors.New("materializer unavailable"),
	}
	tool := findTool(t, HTTPTools(svc, HTTPToolOptions{}), "wow_query")

	result, err := tool.Handler(context.Background(), json.RawMessage(`{"table":"SpellName","region":"eu","product":"wowt","locale":"enUS"}`))
	if err != nil {
		t.Fatalf("wow_query handler should return an envelope, not handler error: %v", err)
	}
	if svc.ensureCalls != 1 {
		t.Fatalf("EnsureTable calls = %d, want 1", svc.ensureCalls)
	}
	if svc.lastTable != "SpellName" {
		t.Fatalf("EnsureTable table = %q, want SpellName", svc.lastTable)
	}
	if svc.lastContext != (httpservice.RequestContext{Region: "eu", Product: "wowt", Locale: "enUS"}) {
		t.Fatalf("EnsureTable context = %#v", svc.lastContext)
	}
	got := result.(map[string]interface{})
	if got["ok"] != false || got["command"] != "query" {
		t.Fatalf("unexpected query envelope: %#v", got)
	}
	if code := got["error"].(map[string]interface{})["code"]; code != "materializer_unavailable" {
		t.Fatalf("error code = %#v, want materializer_unavailable", code)
	}
}

func TestHTTPDB2HandlerQueriesDB2AfterEnsureTable(t *testing.T) {
	svc := &fakeHTTPService{
		queryRows: []map[string]interface{}{{"ID": uint32(123), "Name_lang": "Fireball"}},
	}
	tool := findTool(t, HTTPTools(svc, HTTPToolOptions{}), "wow_query")

	result, err := tool.Handler(context.Background(), json.RawMessage(`{"table":"SpellName","id":123,"field":"ID","filter":"Name_lang LIKE 'Fire%'","limit":1,"fields":["ID","Name_lang"]}`))
	if err != nil {
		t.Fatalf("wow_query handler: %v", err)
	}
	if svc.ensureCalls != 1 {
		t.Fatalf("EnsureTable calls = %d, want 1", svc.ensureCalls)
	}
	if svc.queryCalls != 1 {
		t.Fatalf("QueryDB2 calls = %d, want 1", svc.queryCalls)
	}
	if svc.lastQuery.Table != "SpellName" || svc.lastQuery.IDField != "ID" || len(svc.lastQuery.IDs) != 1 || svc.lastQuery.IDs[0] != 123 {
		t.Fatalf("QueryDB2 query = %#v", svc.lastQuery)
	}
	if svc.lastQuery.Filter != "Name_lang LIKE 'Fire%'" || svc.lastQuery.Limit != 1 {
		t.Fatalf("QueryDB2 filter/limit = %#v", svc.lastQuery)
	}
	if len(svc.lastQuery.Fields) != 2 || svc.lastQuery.Fields[0] != "ID" || svc.lastQuery.Fields[1] != "Name_lang" {
		t.Fatalf("QueryDB2 fields = %#v", svc.lastQuery.Fields)
	}
	got := result.(map[string]interface{})
	if got["ok"] != true || got["command"] != "query rows" {
		t.Fatalf("unexpected query envelope: %#v", got)
	}
	rows := got["data"].(map[string]interface{})["rows"].([]map[string]interface{})
	if len(rows) != 1 || rows[0]["Name_lang"] != "Fireball" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestHTTPDB2HandlerSupportsSearchForeignKeyAndStreamModes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		raw       json.RawMessage
		wantMode  string
		wantField string
		wantIDs   []uint32
		wantLimit int
	}{
		{
			name:      "search",
			raw:       json.RawMessage(`{"mode":"search","table":"SpellName","field":"Name_lang","query":"Fire","limit":3}`),
			wantMode:  "search",
			wantField: "Name_lang",
			wantLimit: 3,
		},
		{
			name:      "foreign-key",
			raw:       json.RawMessage(`{"mode":"foreign-key","table":"SpellEffect","field":"SpellID","value":123}`),
			wantMode:  "foreign-key",
			wantField: "SpellID",
			wantIDs:   []uint32{123},
		},
		{
			name:      "stream",
			raw:       json.RawMessage(`{"mode":"stream","table":"SpellEffect","fields":["ID"],"limit":2}`),
			wantMode:  "stream",
			wantLimit: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeHTTPService{queryRows: []map[string]interface{}{{"ID": uint32(123)}}}
			tool := findTool(t, HTTPTools(svc, HTTPToolOptions{}), "wow_query")

			result, err := tool.Handler(context.Background(), tc.raw)
			if err != nil {
				t.Fatalf("wow_query handler: %v", err)
			}
			if svc.queryCalls != 1 {
				t.Fatalf("QueryDB2 calls = %d, want 1", svc.queryCalls)
			}
			if svc.lastQuery.Table == "" {
				t.Fatalf("QueryDB2 query missing table: %#v", svc.lastQuery)
			}
			if tc.wantField != "" && svc.lastQuery.IDField != tc.wantField {
				t.Fatalf("IDField = %q, want %q", svc.lastQuery.IDField, tc.wantField)
			}
			if len(tc.wantIDs) > 0 && (len(svc.lastQuery.IDs) != len(tc.wantIDs) || svc.lastQuery.IDs[0] != tc.wantIDs[0]) {
				t.Fatalf("IDs = %#v, want %#v", svc.lastQuery.IDs, tc.wantIDs)
			}
			if svc.lastQuery.Limit != tc.wantLimit {
				t.Fatalf("Limit = %d, want %d", svc.lastQuery.Limit, tc.wantLimit)
			}
			data := result.(map[string]interface{})["data"].(map[string]interface{})
			if data["mode"] != tc.wantMode {
				t.Fatalf("mode = %#v, want %q; data=%#v", data["mode"], tc.wantMode, data)
			}
		})
	}
}

func TestHTTPDB2HandlerSupportsSchemaMode(t *testing.T) {
	svc := &fakeHTTPService{
		schema: httpservice.DB2Schema{
			Table:    "SpellName",
			RowCount: 42,
			Fields:   map[string]string{"ID": "INTEGER", "Name_lang": "VARCHAR"},
		},
	}
	tool := findTool(t, HTTPTools(svc, HTTPToolOptions{}), "wow_query")

	result, err := tool.Handler(context.Background(), json.RawMessage(`{"mode":"schema","table":"SpellName"}`))
	if err != nil {
		t.Fatalf("wow_query handler: %v", err)
	}
	if svc.schemaCalls != 1 || svc.lastSchemaTable != "SpellName" {
		t.Fatalf("SchemaDB2 calls/table = %d/%q, want 1/SpellName", svc.schemaCalls, svc.lastSchemaTable)
	}
	data := result.(map[string]interface{})["data"].(map[string]interface{})
	if data["mode"] != "schema" || data["rowCount"] != 42 {
		t.Fatalf("schema data = %#v", data)
	}
}

func TestHTTPDB2HandlerReturnsQueryErrorEnvelope(t *testing.T) {
	svc := &fakeHTTPService{
		queryErr: httpservice.CapabilityError{Code: "invalid_filter", Message: "unsafe filter value"},
	}
	tool := findTool(t, HTTPTools(svc, HTTPToolOptions{}), "wow_query")

	result, err := tool.Handler(context.Background(), json.RawMessage(`{"table":"SpellName","filter":"Name_lang = 'Fireball' OR 1=1"}`))
	if err != nil {
		t.Fatalf("wow_query handler: %v", err)
	}
	got := result.(map[string]interface{})
	if got["ok"] != false {
		t.Fatalf("query should return structured error, got %#v", got)
	}
	if code := got["error"].(map[string]interface{})["code"]; code != "invalid_filter" {
		t.Fatalf("error code = %#v, want invalid_filter", code)
	}
}

func TestHTTPIconHandlerMapsArtifactLinkResult(t *testing.T) {
	svc := &fakeHTTPService{}
	linker := &fakeArtifactLinker{
		link: ArtifactLink{
			Path:        `D:\cache\icons\134400.png`,
			URI:         "https://mcp.example.com/files/icons/134400.png",
			DownloadURL: "https://mcp.example.com/files/icons/134400.png",
			MimeType:    "image/png",
			Name:        "134400.png",
			Size:        123,
			SHA256:      "abc123",
		},
	}
	tool := findTool(t, HTTPTools(svc, HTTPToolOptions{Artifacts: linker}), "wow_icon")

	result, err := tool.Handler(context.Background(), json.RawMessage(`{"path":"D:\\cache\\icons\\134400.png","mimeType":"image/png"}`))
	if err != nil {
		t.Fatalf("wow_icon handler: %v", err)
	}
	if linker.linkCalls != 1 {
		t.Fatalf("LinkArtifact calls = %d, want 1", linker.linkCalls)
	}
	data := result.(map[string]interface{})["data"].(map[string]interface{})
	if data["uri"] != "https://mcp.example.com/files/icons/134400.png" || data["sha256"] != "abc123" {
		t.Fatalf("icon artifact mapping mismatch: %#v", data)
	}
}

func TestHTTPIconHandlerReturnsCapabilityErrorWithoutExportPath(t *testing.T) {
	svc := &fakeHTTPService{
		capabilityErr: httpservice.CapabilityError{Code: "export_engine_unavailable", Message: "icon export unavailable"},
	}
	tool := findTool(t, HTTPTools(svc, HTTPToolOptions{}), "wow_icon")

	result, err := tool.Handler(context.Background(), json.RawMessage(`{"fileDataID":134400}`))
	if err != nil {
		t.Fatalf("wow_icon handler: %v", err)
	}
	if svc.capabilityCalls != 1 || svc.lastCapability != "icon_export" {
		t.Fatalf("RequireCapability calls/capability = %d/%q", svc.capabilityCalls, svc.lastCapability)
	}
	got := result.(map[string]interface{})
	if got["ok"] != false {
		t.Fatalf("icon should return structured error, got %#v", got)
	}
	if code := got["error"].(map[string]interface{})["code"]; code != "export_engine_unavailable" {
		t.Fatalf("error code = %#v, want export_engine_unavailable", code)
	}
}

func TestHTTPFileGetExportsArtifactFromAssetProvider(t *testing.T) {
	root := t.TempDir()
	outPath := filepath.Join(root, "files", "134400.bin")
	svc := &fakeHTTPService{}
	assets := &fakeAssetProvider{fileStore: fakeAssetFileStore{data: []byte("asset-bytes")}}
	linker := &fakeArtifactLinker{
		reservePath: outPath,
		reserveLink: ArtifactLink{
			Path:        outPath,
			URI:         "http://example.test/files/134400.bin",
			DownloadURL: "http://example.test/files/134400.bin",
			MimeType:    "application/octet-stream",
			Name:        "134400.bin",
		},
	}
	tool := findTool(t, HTTPTools(svc, HTTPToolOptions{Assets: assets, Artifacts: linker}), "wow_file")

	result, err := tool.Handler(context.Background(), json.RawMessage(`{"mode":"get","fileDataID":134400}`))
	if err != nil {
		t.Fatalf("wow_file get handler: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read exported artifact: %v", err)
	}
	if string(data) != "asset-bytes" {
		t.Fatalf("artifact data = %q", data)
	}
	payload := result.(map[string]interface{})["data"].(map[string]interface{})
	if payload["downloadUrl"] != "http://example.test/files/134400.bin" || payload["size"] != 11 {
		t.Fatalf("file artifact payload = %#v", payload)
	}
}

func TestHTTPBusinessHandlersEnsureTablesAndReturnStructuredMaterializerErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		wantTable string
	}{
		{"wow_item", "Item"},
		{"wow_spell", "SpellName"},
		{"wow_creature", "CreatureDisplayInfo"},
		{"wow_encounter", "JournalEncounterSection"},
		{"wow_decor", "HouseDecor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeHTTPService{
				ensureErr: httpservice.CapabilityError{Code: "query_engine_unavailable", Message: "query engine unavailable"},
			}
			tool := findTool(t, HTTPTools(svc, HTTPToolOptions{}), tc.name)

			result, err := tool.Handler(context.Background(), json.RawMessage(`{"region":"cn","product":"wow","locale":"zhCN"}`))
			if err != nil {
				t.Fatalf("%s handler: %v", tc.name, err)
			}
			if svc.ensureCalls != 1 || svc.lastTable != tc.wantTable {
				t.Fatalf("EnsureTable calls/table = %d/%q, want 1/%q", svc.ensureCalls, svc.lastTable, tc.wantTable)
			}
			got := result.(map[string]interface{})
			if got["ok"] != false {
				t.Fatalf("%s should return structured error, got %#v", tc.name, got)
			}
			if code := got["error"].(map[string]interface{})["code"]; code != "query_engine_unavailable" {
				t.Fatalf("%s error code = %#v, want query_engine_unavailable", tc.name, code)
			}
		})
	}
}

func TestHTTPFileAndVideoRemainStructuredCapabilityErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		capability string
	}{
		{"wow_file", "file_query"},
		{"wow_video", "video_query"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeHTTPService{
				capabilityErr: httpservice.CapabilityError{Code: "query_engine_unavailable", Message: "query engine unavailable"},
			}
			tool := findTool(t, HTTPTools(svc, HTTPToolOptions{}), tc.name)

			result, err := tool.Handler(context.Background(), json.RawMessage(`{"region":"cn","product":"wow","locale":"zhCN"}`))
			if err != nil {
				t.Fatalf("%s handler: %v", tc.name, err)
			}
			if svc.capabilityCalls != 1 || svc.lastCapability != tc.capability {
				t.Fatalf("RequireCapability calls/capability = %d/%q, want 1/%q", svc.capabilityCalls, svc.lastCapability, tc.capability)
			}
			got := result.(map[string]interface{})
			if got["ok"] != false {
				t.Fatalf("%s should return structured error, got %#v", tc.name, got)
			}
		})
	}
}

type fakeHTTPService struct {
	status          httpservice.Status
	statusCalls     int
	builds          httpservice.BuildCatalog
	buildCalls      int
	ensureErr       error
	ensureCalls     int
	lastContext     httpservice.RequestContext
	lastTable       string
	capabilityErr   error
	capabilityCalls int
	lastCapability  string
	queryRows       []map[string]interface{}
	queryErr        error
	queryCalls      int
	lastQuery       httpservice.DB2Query
	schema          httpservice.DB2Schema
	schemaErr       error
	schemaCalls     int
	lastSchemaTable string
}

func (f *fakeHTTPService) Status() httpservice.Status {
	f.statusCalls++
	return f.status
}

func (f *fakeHTTPService) Builds() httpservice.BuildCatalog {
	f.buildCalls++
	return f.builds
}

func (f *fakeHTTPService) EnsureTable(ctx context.Context, rc httpservice.RequestContext, table string) error {
	f.ensureCalls++
	f.lastContext = rc
	f.lastTable = table
	return f.ensureErr
}

func (f *fakeHTTPService) RequireCapability(ctx context.Context, rc httpservice.RequestContext, capability string) error {
	f.capabilityCalls++
	f.lastContext = rc
	f.lastCapability = capability
	return f.capabilityErr
}

func (f *fakeHTTPService) QueryDB2(ctx context.Context, query httpservice.DB2Query) ([]map[string]interface{}, error) {
	f.queryCalls++
	f.lastQuery = query
	return f.queryRows, f.queryErr
}

func (f *fakeHTTPService) SchemaDB2(ctx context.Context, rc httpservice.RequestContext, table string) (httpservice.DB2Schema, error) {
	f.schemaCalls++
	f.lastContext = rc
	f.lastSchemaTable = table
	return f.schema, f.schemaErr
}

type fakeArtifactLinker struct {
	link        ArtifactLink
	linkCalls   int
	path        string
	mimeType    string
	reservePath string
	reserveLink ArtifactLink
}

func (f *fakeArtifactLinker) LinkArtifact(path, mimeType string) (ArtifactLink, error) {
	f.linkCalls++
	f.path = path
	f.mimeType = mimeType
	return f.link, nil
}

func (f *fakeArtifactLinker) ReserveArtifact(kind, name, mimeType string) (string, ArtifactLink, error) {
	if err := os.MkdirAll(filepath.Dir(f.reservePath), 0755); err != nil {
		return "", ArtifactLink{}, err
	}
	return f.reservePath, f.reserveLink, nil
}

type fakeAssetProvider struct {
	fileStore appruntime.FileStore
	iconStore appruntime.IconStore
}

func (f *fakeAssetProvider) FileStore(context.Context, httpservice.RequestContext, bool) (appruntime.FileStore, error) {
	return f.fileStore, nil
}

func (f *fakeAssetProvider) IconStore(context.Context, httpservice.RequestContext) (appruntime.IconStore, error) {
	return f.iconStore, nil
}

type fakeAssetFileStore struct {
	data []byte
}

func (f fakeAssetFileStore) Lookup(fileDataID uint32) (string, bool) {
	return "interface/icons/test.blp", true
}

func (f fakeAssetFileStore) Search(query string, limit int) []appruntime.FileEntry {
	return []appruntime.FileEntry{{FileDataID: 134400, Filename: "interface/icons/test.blp"}}
}

func (f fakeAssetFileStore) SearchCount(query string) int { return 1 }

func (f fakeAssetFileStore) Extension(extension string, limit int) []appruntime.FileEntry {
	return []appruntime.FileEntry{{FileDataID: 134400, Filename: "interface/icons/test.blp"}}
}

func (f fakeAssetFileStore) ExtensionCount(extension string) int { return 1 }

func (f fakeAssetFileStore) ExistsByID(fileDataID uint32) bool { return true }

func (f fakeAssetFileStore) ExistsByName(filename string) bool { return filename != "" }

func (f fakeAssetFileStore) EncodingInfo(fileDataID uint32) (interface{}, error) {
	return map[string]interface{}{"fileDataID": fileDataID}, nil
}

func (f fakeAssetFileStore) ReadByID(fileDataID uint32) ([]byte, error) {
	return f.data, nil
}

func (f fakeAssetFileStore) ReadByName(filename string) ([]byte, error) {
	return f.data, nil
}

func findTool(t *testing.T, tools []mcpserver.Tool, name string) mcpserver.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %s not found", name)
	return mcpserver.Tool{}
}
