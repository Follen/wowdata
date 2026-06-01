package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"wowdata/internal/mcpserver"
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
		"wow_db2",
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
	tool := findTool(t, HTTPTools(svc, HTTPToolOptions{}), "wow_db2")

	result, err := tool.Handler(context.Background(), json.RawMessage(`{"table":"SpellName","region":"eu","product":"wowt","locale":"enUS"}`))
	if err != nil {
		t.Fatalf("wow_db2 handler should return an envelope, not handler error: %v", err)
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
	if got["ok"] != false || got["command"] != "db2" {
		t.Fatalf("unexpected db2 envelope: %#v", got)
	}
	if code := got["error"].(map[string]interface{})["code"]; code != "materializer_unavailable" {
		t.Fatalf("error code = %#v, want materializer_unavailable", code)
	}
}

func TestHTTPDB2HandlerQueriesDB2AfterEnsureTable(t *testing.T) {
	svc := &fakeHTTPService{
		queryRows: []map[string]interface{}{{"ID": uint32(123), "Name_lang": "Fireball"}},
	}
	tool := findTool(t, HTTPTools(svc, HTTPToolOptions{}), "wow_db2")

	result, err := tool.Handler(context.Background(), json.RawMessage(`{"table":"SpellName","id":123,"field":"ID","filter":"Name_lang LIKE 'Fire%'","limit":1,"fields":["ID","Name_lang"]}`))
	if err != nil {
		t.Fatalf("wow_db2 handler: %v", err)
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
	if got["ok"] != true || got["command"] != "db2" {
		t.Fatalf("unexpected db2 envelope: %#v", got)
	}
	rows := got["data"].(map[string]interface{})["rows"].([]map[string]interface{})
	if len(rows) != 1 || rows[0]["Name_lang"] != "Fireball" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestHTTPDB2HandlerReturnsQueryErrorEnvelope(t *testing.T) {
	svc := &fakeHTTPService{
		queryErr: httpservice.CapabilityError{Code: "invalid_filter", Message: "unsafe filter value"},
	}
	tool := findTool(t, HTTPTools(svc, HTTPToolOptions{}), "wow_db2")

	result, err := tool.Handler(context.Background(), json.RawMessage(`{"table":"SpellName","filter":"Name_lang = 'Fireball' OR 1=1"}`))
	if err != nil {
		t.Fatalf("wow_db2 handler: %v", err)
	}
	got := result.(map[string]interface{})
	if got["ok"] != false {
		t.Fatalf("db2 should return structured error, got %#v", got)
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

func TestHTTPBusinessHandlersReturnStructuredCapabilityErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		capability string
	}{
		{"wow_item", "item_query"},
		{"wow_spell", "spell_query"},
		{"wow_file", "file_query"},
		{"wow_creature", "creature_query"},
		{"wow_encounter", "encounter_query"},
		{"wow_decor", "decor_query"},
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
			if code := got["error"].(map[string]interface{})["code"]; code != "query_engine_unavailable" {
				t.Fatalf("%s error code = %#v, want query_engine_unavailable", tc.name, code)
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

type fakeArtifactLinker struct {
	link      ArtifactLink
	linkCalls int
	path      string
	mimeType  string
}

func (f *fakeArtifactLinker) LinkArtifact(path, mimeType string) (ArtifactLink, error) {
	f.linkCalls++
	f.path = path
	f.mimeType = mimeType
	return f.link, nil
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
