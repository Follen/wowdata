package mcphttp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"wowdata/internal/mcpserver"
	"wowdata/internal/server/health"
	"wowdata/internal/server/service"
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
