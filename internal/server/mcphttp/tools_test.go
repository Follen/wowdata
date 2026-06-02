package mcphttp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"wowdata/internal/server/health"
	"wowdata/internal/server/service"
	"wowdata/internal/shared/mcpserver"
)

func TestHTTPToolListIncludesDefaultServerTools(t *testing.T) {
	tools := HTTPTools(Options{
		QueryService: service.UnavailableQueryService{},
	})

	got := toolNames(tools)
	wantPresent := []string{
		"wow_status",
		"wow_builds",
		"wow_query",
		"wow_item",
		"wow_spell",
		"wow_file",
		"wow_icon",
		"wow_creature",
		"wow_encounter",
		"wow_decor",
		"wow_video",
	}
	for _, want := range wantPresent {
		if !contains(got, want) {
			t.Fatalf("HTTPTools missing %q in %v", want, got)
		}
	}
	for _, forbidden := range []string{"wow_warmup", "wow_db2"} {
		if contains(got, forbidden) {
			t.Fatalf("HTTPTools included forbidden tool %q in %v", forbidden, got)
		}
	}
}

func TestWowQueryModesDispatchToQueryService(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]interface{}
		want    string
		called  string
		wantReq interface{}
	}{
		{
			name:   "tables",
			args:   map[string]interface{}{"mode": "tables", "region": "us", "product": "wow", "locale": "enUS", "buildKey": "11.1.7.61491"},
			want:   "query tables",
			called: "tables",
			wantReq: service.TablesRequest{
				Context: service.RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "11.1.7.61491"},
			},
		},
		{
			name:   "schema",
			args:   map[string]interface{}{"mode": "schema", "table": "Item", "region": "us", "product": "wow", "locale": "enUS", "buildKey": "11.1.7.61491"},
			want:   "query schema",
			called: "schema",
			wantReq: service.SchemaRequest{
				Context: service.RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "11.1.7.61491"},
				Table:   "Item",
			},
		},
		{
			name:   "rows",
			args:   map[string]interface{}{"mode": "rows", "table": "Item", "ids": []interface{}{float64(1), float64(2)}, "field": "ID", "fields": []interface{}{"ID", "Name"}, "filter": "ID > 0", "limit": float64(5), "offset": float64(1)},
			want:   "query rows",
			called: "rows",
			wantReq: service.QueryRowsRequest{
				Table:   "Item",
				IDs:     []uint64{1, 2},
				IDField: "ID",
				Fields:  []string{"ID", "Name"},
				Filter:  "ID > 0",
				Limit:   5,
				Offset:  1,
			},
		},
		{
			name:   "search",
			args:   map[string]interface{}{"mode": "search", "table": "SpellName", "field": "Name_lang", "query": "fire", "limit": float64(3)},
			want:   "query search",
			called: "search",
			wantReq: service.SearchRequest{
				Table: "SpellName",
				Field: "Name_lang",
				Query: "fire",
				Limit: 3,
			},
		},
		{
			name:   "foreign-key",
			args:   map[string]interface{}{"mode": "foreign-key", "table": "ItemEffect", "field": "ParentItemID", "value": float64(19019), "limit": float64(7)},
			want:   "query foreign-key",
			called: "foreign-key",
			wantReq: service.ForeignKeyRequest{
				Table: "ItemEffect",
				Field: "ParentItemID",
				Value: float64(19019),
				Limit: 7,
			},
		},
		{
			name:   "stream",
			args:   map[string]interface{}{"mode": "stream", "table": "CreatureDisplayInfo", "limit": float64(9), "offset": float64(2)},
			want:   "query stream",
			called: "stream",
			wantReq: service.StreamRequest{
				Table:  "CreatureDisplayInfo",
				Limit:  9,
				Offset: 2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeQueryService{}
			tool := findTool(t, HTTPTools(Options{QueryService: fake}), "wow_query")
			raw, err := json.Marshal(tt.args)
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}

			result, err := tool.Handler(context.Background(), raw)
			if err != nil {
				t.Fatalf("wow_query handler: %v", err)
			}
			envelope, ok := result.(map[string]interface{})
			if !ok {
				t.Fatalf("result = %T, want map envelope", result)
			}
			if envelope["ok"] != true {
				t.Fatalf("ok = %v, want true; result=%#v", envelope["ok"], envelope)
			}
			if envelope["command"] != tt.want {
				t.Fatalf("command = %v, want %q", envelope["command"], tt.want)
			}
			if fake.called != tt.called {
				t.Fatalf("called = %q, want %q", fake.called, tt.called)
			}
			if !reflect.DeepEqual(fake.lastRequest, tt.wantReq) {
				t.Fatalf("request = %#v, want %#v", fake.lastRequest, tt.wantReq)
			}
			if tt.called == "tables" {
				data, ok := envelope["data"].(map[string]interface{})
				if !ok {
					t.Fatalf("data = %T, want map", envelope["data"])
				}
				if data["mode"] != "tables" {
					t.Fatalf("data mode = %v, want tables", data["mode"])
				}
				if data["count"] != 2 {
					t.Fatalf("data count = %v, want 2", data["count"])
				}
				tables, ok := data["tables"].([]service.TableInfo)
				if !ok {
					t.Fatalf("tables = %T, want []service.TableInfo", data["tables"])
				}
				if len(tables) != 2 || tables[0].Name != "Item" || tables[1].Name != "SpellName" {
					t.Fatalf("tables = %#v", tables)
				}
			}
		})
	}
}

func TestWowQueryInvalidModeMentionsTables(t *testing.T) {
	fake := &fakeQueryService{}
	tool := findTool(t, HTTPTools(Options{QueryService: fake}), "wow_query")

	result, err := tool.Handler(context.Background(), json.RawMessage(`{"mode":"wat","table":"Item"}`))
	if err != nil {
		t.Fatalf("wow_query handler: %v", err)
	}
	envelope := resultEnvelope(t, result)
	errInfo, ok := envelope["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error = %T, want map", envelope["error"])
	}
	message, ok := errInfo["message"].(string)
	if !ok {
		t.Fatalf("error message = %T, want string", errInfo["message"])
	}
	if !strings.Contains(message, "tables") {
		t.Fatalf("message = %q, want tables in supported modes", message)
	}
}

func TestWowQueryStreamAppliesBoundedDefaultLimit(t *testing.T) {
	fake := &fakeQueryService{}
	tool := findTool(t, HTTPTools(Options{QueryService: fake}), "wow_query")

	result, err := tool.Handler(context.Background(), json.RawMessage(`{"mode":"stream","table":"Item"}`))
	if err != nil {
		t.Fatalf("wow_query handler: %v", err)
	}
	envelope := resultEnvelope(t, result)
	if envelope["ok"] != true {
		t.Fatalf("ok = %v, want true; result=%#v", envelope["ok"], envelope)
	}
	req, ok := fake.lastRequest.(service.StreamRequest)
	if !ok {
		t.Fatalf("lastRequest = %T, want StreamRequest", fake.lastRequest)
	}
	if req.Limit <= 0 {
		t.Fatalf("stream limit = %d, want bounded default", req.Limit)
	}
}

func TestHTTPBusinessToolsDispatchToBoundedServerAssemblers(t *testing.T) {
	tests := []struct {
		name      string
		tool      string
		args      map[string]interface{}
		command   string
		wantCalls []businessToolCall
	}{
		{
			name:    "item",
			tool:    "wow_item",
			args:    map[string]interface{}{"itemID": float64(19019)},
			command: "item",
			wantCalls: []businessToolCall{
				{table: "Item", idField: "ID"},
				{table: "ItemSparse", idField: "ID"},
			},
		},
		{
			name:    "spell",
			tool:    "wow_spell",
			args:    map[string]interface{}{"spellID": float64(100), "maxDepth": float64(1)},
			command: "spell",
			wantCalls: []businessToolCall{
				{table: "SpellEffect", idField: "SpellID"},
				{table: "Spell", idField: "ID"},
				{table: "SpellName", idField: "ID"},
				{table: "SpellMisc", idField: "SpellID"},
			},
		},
		{
			name:    "creature",
			tool:    "wow_creature",
			args:    map[string]interface{}{"displayID": float64(100)},
			command: "creature",
			wantCalls: []businessToolCall{
				{table: "CreatureDisplayInfo", idField: "ID"},
				{table: "CreatureDisplayInfoGeosetData", idField: "CreatureDisplayInfoID"},
				{table: "CreatureModelData", idField: "ID"},
			},
		},
		{
			name:    "encounter",
			tool:    "wow_encounter",
			args:    map[string]interface{}{"journalEncounterID": float64(900)},
			command: "encounter",
			wantCalls: []businessToolCall{
				{table: "JournalEncounterSection", idField: "JournalEncounterID"},
			},
		},
		{
			name:    "decor",
			tool:    "wow_decor",
			args:    map[string]interface{}{"id": float64(77)},
			command: "decor",
			wantCalls: []businessToolCall{
				{table: "HouseDecor", idField: "ID"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeBusinessQueryService()
			tool := findTool(t, HTTPTools(Options{QueryService: fake}), tt.tool)
			raw, err := json.Marshal(tt.args)
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}

			result, err := tool.Handler(context.Background(), raw)
			if err != nil {
				t.Fatalf("%s handler: %v", tt.tool, err)
			}
			envelope := resultEnvelope(t, result)
			if envelope["ok"] != true {
				t.Fatalf("ok = %v, want true; result=%#v", envelope["ok"], envelope)
			}
			if envelope["command"] != tt.command {
				t.Fatalf("command = %v, want %q", envelope["command"], tt.command)
			}
			if _, ok := envelope["data"].(map[string]interface{}); !ok {
				t.Fatalf("data = %T, want map", envelope["data"])
			}
			for _, want := range tt.wantCalls {
				if !fake.sawBoundedCall(want.table, want.idField) {
					t.Fatalf("%s did not issue bounded %s.%s query; calls=%#v", tt.tool, want.table, want.idField, fake.calls)
				}
			}
		})
	}
}

func TestHTTPBusinessToolsReportQueryErrorsTruthfully(t *testing.T) {
	fake := &errorBusinessQueryService{err: errors.New("table Item is not ready")}
	tool := findTool(t, HTTPTools(Options{QueryService: fake}), "wow_item")

	result, err := tool.Handler(context.Background(), json.RawMessage(`{"itemID":19019}`))
	if err != nil {
		t.Fatalf("wow_item handler: %v", err)
	}
	envelope := resultEnvelope(t, result)
	if envelope["ok"] != false {
		t.Fatalf("ok = %v, want false; result=%#v", envelope["ok"], envelope)
	}
	errInfo, ok := envelope["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error = %T, want map", envelope["error"])
	}
	if errInfo["code"] == "invalid_request" {
		t.Fatalf("query service failure was reported as invalid_request: %#v", envelope)
	}
	if errInfo["code"] != "query_engine_unavailable" {
		t.Fatalf("error code = %v, want query_engine_unavailable; result=%#v", errInfo["code"], envelope)
	}
}

func TestHTTPBusinessToolsRejectMissingLookupArguments(t *testing.T) {
	tests := []struct {
		name string
		tool string
		raw  json.RawMessage
	}{
		{name: "item", tool: "wow_item", raw: json.RawMessage(`{}`)},
		{name: "spell", tool: "wow_spell", raw: json.RawMessage(`{}`)},
		{name: "creature", tool: "wow_creature", raw: json.RawMessage(`{}`)},
		{name: "encounter", tool: "wow_encounter", raw: json.RawMessage(`{}`)},
		{name: "decor", tool: "wow_decor", raw: json.RawMessage(`{}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeBusinessQueryService()
			tool := findTool(t, HTTPTools(Options{QueryService: fake}), tt.tool)

			result, err := tool.Handler(context.Background(), tt.raw)
			if err != nil {
				t.Fatalf("%s handler: %v", tt.tool, err)
			}
			envelope := resultEnvelope(t, result)
			if envelope["ok"] != false {
				t.Fatalf("ok = %v, want false; result=%#v", envelope["ok"], envelope)
			}
			errInfo, ok := envelope["error"].(map[string]interface{})
			if !ok {
				t.Fatalf("error = %T, want map", envelope["error"])
			}
			if errInfo["code"] != "invalid_request" {
				t.Fatalf("error code = %v, want invalid_request; result=%#v", errInfo["code"], envelope)
			}
			if len(fake.calls) != 0 {
				t.Fatalf("service calls = %#v, want no calls for missing lookup args", fake.calls)
			}
		})
	}
}

func TestWowBuildsReturnsHealthSnapshotContexts(t *testing.T) {
	provider := &fakeHealthProvider{
		snapshot: health.Snapshot{
			Contexts: []health.ContextStatus{
				{Label: "US Retail", Region: "us", Product: "wow", Locale: "enUS", State: health.StateReady, ActiveBuild: "11.1.7.61491"},
				{Label: "CN Classic", Region: "cn", Product: "wow_classic", Locale: "zhCN", State: health.StatePreparing, ActiveBuild: "1.15.7.61491"},
			},
		},
	}
	tool := findTool(t, HTTPTools(Options{HealthProvider: provider}), "wow_builds")

	result, err := tool.Handler(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("wow_builds handler: %v", err)
	}
	envelope := resultEnvelope(t, result)
	if envelope["ok"] != true {
		t.Fatalf("ok = %v, want true; result=%#v", envelope["ok"], envelope)
	}
	if envelope["command"] != "builds" {
		t.Fatalf("command = %v, want builds", envelope["command"])
	}
	data, ok := envelope["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data = %T, want map", envelope["data"])
	}
	contexts, ok := data["contexts"].([]health.ContextStatus)
	if !ok {
		t.Fatalf("contexts = %T, want []health.ContextStatus", data["contexts"])
	}
	if len(contexts) != 2 {
		t.Fatalf("contexts length = %d, want 2", len(contexts))
	}
	if contexts[0].Label != "US Retail" || contexts[0].ActiveBuild != "11.1.7.61491" {
		t.Fatalf("first context = %#v", contexts[0])
	}
	if contexts[1].Label != "CN Classic" || contexts[1].State != health.StatePreparing {
		t.Fatalf("second context = %#v", contexts[1])
	}
}

func TestWowQueryRowsRejectsInvalidNumericArgs(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "fractional limit",
			args: map[string]interface{}{"mode": "rows", "table": "Item", "limit": float64(1.9)},
		},
		{
			name: "negative limit",
			args: map[string]interface{}{"mode": "rows", "table": "Item", "limit": float64(-1)},
		},
		{
			name: "malformed limit",
			args: map[string]interface{}{"mode": "rows", "table": "Item", "limit": "bad"},
		},
		{
			name: "fractional id",
			args: map[string]interface{}{"mode": "rows", "table": "Item", "id": float64(1.9)},
		},
		{
			name: "negative id",
			args: map[string]interface{}{"mode": "rows", "table": "Item", "id": float64(-1)},
		},
		{
			name: "malformed ids",
			args: map[string]interface{}{"mode": "rows", "table": "Item", "ids": []interface{}{"abc"}},
		},
		{
			name: "fractional offset",
			args: map[string]interface{}{"mode": "rows", "table": "Item", "offset": float64(2.5)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeQueryService{}
			tool := findTool(t, HTTPTools(Options{QueryService: fake}), "wow_query")
			raw, err := json.Marshal(tt.args)
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}

			result, err := tool.Handler(context.Background(), raw)
			if err != nil {
				t.Fatalf("wow_query handler: %v", err)
			}
			envelope := resultEnvelope(t, result)
			if envelope["ok"] != false {
				t.Fatalf("ok = %v, want false; result=%#v", envelope["ok"], envelope)
			}
			if envelope["command"] != "query rows" {
				t.Fatalf("command = %v, want query rows", envelope["command"])
			}
			errInfo, ok := envelope["error"].(map[string]interface{})
			if !ok {
				t.Fatalf("error = %T, want map", envelope["error"])
			}
			if errInfo["code"] != "invalid_request" {
				t.Fatalf("error code = %v, want invalid_request; result=%#v", errInfo["code"], envelope)
			}
			if fake.called != "" {
				t.Fatalf("service called = %q, want no call", fake.called)
			}
		})
	}
}

type fakeQueryService struct {
	called      string
	lastRequest interface{}
}

func (f *fakeQueryService) Schema(ctx context.Context, req service.SchemaRequest) (service.Schema, error) {
	f.called = "schema"
	f.lastRequest = req
	return service.Schema{Table: req.Table, RowCount: 2, Fields: []service.Field{{Name: "ID", Type: "uint"}}}, nil
}

func (f *fakeQueryService) Tables(ctx context.Context, req service.TablesRequest) (service.TableCatalog, error) {
	f.called = "tables"
	f.lastRequest = req
	return service.TableCatalog{Tables: []service.TableInfo{{Name: "Item"}, {Name: "SpellName"}}}, nil
}

func (f *fakeQueryService) Rows(ctx context.Context, req service.QueryRowsRequest) ([]map[string]interface{}, error) {
	f.called = "rows"
	f.lastRequest = req
	return []map[string]interface{}{{"ID": float64(1)}}, nil
}

func (f *fakeQueryService) Search(ctx context.Context, req service.SearchRequest) ([]map[string]interface{}, error) {
	f.called = "search"
	f.lastRequest = req
	return []map[string]interface{}{{"Name_lang": "fire"}}, nil
}

func (f *fakeQueryService) ForeignKey(ctx context.Context, req service.ForeignKeyRequest) ([]map[string]interface{}, error) {
	f.called = "foreign-key"
	f.lastRequest = req
	return []map[string]interface{}{{"ParentItemID": req.Value}}, nil
}

func (f *fakeQueryService) Stream(ctx context.Context, req service.StreamRequest) ([]map[string]interface{}, error) {
	f.called = "stream"
	f.lastRequest = req
	return []map[string]interface{}{{"ID": float64(10)}}, nil
}

type businessToolCall struct {
	table   string
	idField string
}

type fakeBusinessQueryService struct {
	rows  map[string][]map[string]interface{}
	calls []businessToolCall
}

func newFakeBusinessQueryService() *fakeBusinessQueryService {
	return &fakeBusinessQueryService{rows: map[string][]map[string]interface{}{
		"Item":                          {{"ID": uint32(19019), "ClassID": int32(2), "SubclassID": int32(7)}},
		"ItemSparse":                    {{"ID": uint32(19019), "Display_lang": "Thunderfury", "InventoryType": int32(21), "OverallQualityID": int32(5)}},
		"SpellEffect":                   {{"SpellID": uint32(100), "EffectTriggerSpell": uint32(200), "EffectIndex": int32(0)}},
		"Spell":                         {{"ID": uint32(100), "Description_lang": "Seed"}, {"ID": uint32(200), "Description_lang": "Child"}},
		"SpellName":                     {{"ID": uint32(100), "Name_lang": "Seed Spell"}, {"ID": uint32(200), "Name_lang": "Child Spell"}},
		"SpellMisc":                     {{"SpellID": uint32(100)}, {"SpellID": uint32(200)}},
		"CreatureDisplayInfo":           {{"ID": uint32(100), "ModelID": uint32(10), "TextureVariationFileDataID": []uint32{6000}}},
		"CreatureDisplayInfoGeosetData": {{"CreatureDisplayInfoID": uint32(100), "GeosetIndex": uint32(1), "GeosetValue": uint32(2)}},
		"CreatureModelData":             {{"ID": uint32(10), "FileDataID": uint32(5000)}},
		"JournalEncounterSection":       {{"ID": uint32(10), "JournalEncounterID": uint32(900), "Title_lang": "Boss", "SpellID": uint32(100)}},
		"HouseDecor":                    {{"ID": uint32(77), "Name_lang": "Banner", "ModelFileDataID": uint32(888)}},
	}}
}

func (f *fakeBusinessQueryService) Schema(context.Context, service.SchemaRequest) (service.Schema, error) {
	return service.Schema{}, nil
}

func (f *fakeBusinessQueryService) Tables(context.Context, service.TablesRequest) (service.TableCatalog, error) {
	return service.TableCatalog{}, nil
}

func (f *fakeBusinessQueryService) Rows(_ context.Context, req service.QueryRowsRequest) ([]map[string]interface{}, error) {
	f.calls = append(f.calls, businessToolCall{table: req.Table, idField: req.IDField})
	if len(req.IDs) == 0 {
		return nil, nil
	}
	idSet := map[uint32]bool{}
	for _, id := range req.IDs {
		idSet[uint32(id)] = true
	}
	out := []map[string]interface{}{}
	for _, row := range f.rows[req.Table] {
		if idSet[rowTestUint32(row[req.IDField])] {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeBusinessQueryService) Search(context.Context, service.SearchRequest) ([]map[string]interface{}, error) {
	return nil, nil
}

func (f *fakeBusinessQueryService) ForeignKey(context.Context, service.ForeignKeyRequest) ([]map[string]interface{}, error) {
	return nil, nil
}

func (f *fakeBusinessQueryService) Stream(context.Context, service.StreamRequest) ([]map[string]interface{}, error) {
	return nil, nil
}

func (f *fakeBusinessQueryService) sawBoundedCall(table, idField string) bool {
	for _, call := range f.calls {
		if call.table == table && call.idField == idField {
			return true
		}
	}
	return false
}

func rowTestUint32(value interface{}) uint32 {
	switch v := value.(type) {
	case uint32:
		return v
	case int32:
		return uint32(v)
	case int:
		return uint32(v)
	case float64:
		return uint32(v)
	default:
		return 0
	}
}

type errorBusinessQueryService struct {
	err error
}

func (f *errorBusinessQueryService) Schema(context.Context, service.SchemaRequest) (service.Schema, error) {
	return service.Schema{}, f.err
}

func (f *errorBusinessQueryService) Tables(context.Context, service.TablesRequest) (service.TableCatalog, error) {
	return service.TableCatalog{}, f.err
}

func (f *errorBusinessQueryService) Rows(context.Context, service.QueryRowsRequest) ([]map[string]interface{}, error) {
	return nil, f.err
}

func (f *errorBusinessQueryService) Search(context.Context, service.SearchRequest) ([]map[string]interface{}, error) {
	return nil, f.err
}

func (f *errorBusinessQueryService) ForeignKey(context.Context, service.ForeignKeyRequest) ([]map[string]interface{}, error) {
	return nil, f.err
}

func (f *errorBusinessQueryService) Stream(context.Context, service.StreamRequest) ([]map[string]interface{}, error) {
	return nil, f.err
}

func toolNames(tools []mcpserver.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func findTool(t *testing.T, tools []mcpserver.Tool, name string) mcpserver.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found in %v", name, toolNames(tools))
	return mcpserver.Tool{}
}

func resultEnvelope(t *testing.T, result interface{}) map[string]interface{} {
	t.Helper()
	envelope, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("result = %T, want map envelope", result)
	}
	return envelope
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
