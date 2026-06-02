package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	serverbootstrap "wowdata/internal/server/bootstrap"
	"wowdata/internal/server/health"
	"wowdata/internal/server/storage/metadata"
)

func TestServerHTTPHelpExposesHTTPCommand(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "mcp", "http", "--help")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("wowdata-server mcp http --help failed: %v\n%s", err, out.String())
	}

	output := out.String()
	for _, want := range []string{
		"Serve MCP tools over Streamable HTTP.",
		"--config",
		"--host",
		"--port",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("help output missing %q\n%s", want, output)
		}
	}
}

func TestServerHTTPCommandInvokesRunnerWithConfig(t *testing.T) {
	var got httpOptions
	called := false
	cmd := newRootCommandWithHTTPRunner(func(opts httpOptions) error {
		called = true
		got = opts
		return nil
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"mcp",
		"http",
		"--config", "/etc/wowdata/http-mcp.yaml",
		"--host", "0.0.0.0",
		"--port", "11223",
		"--artifact-root", "/srv/wowdata/artifacts",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute mcp http: %v", err)
	}
	if !called {
		t.Fatal("HTTP command runner was not called")
	}
	if got.ConfigPath != "/etc/wowdata/http-mcp.yaml" {
		t.Fatalf("ConfigPath = %q", got.ConfigPath)
	}
	if got.Host != "0.0.0.0" {
		t.Fatalf("Host = %q", got.Host)
	}
	if got.Port != 11223 {
		t.Fatalf("Port = %d", got.Port)
	}
	if !got.HostExplicit {
		t.Fatal("HostExplicit = false, want true for --host")
	}
	if !got.PortExplicit {
		t.Fatal("PortExplicit = false, want true for --port")
	}
	if got.ArtifactRoot != "/srv/wowdata/artifacts" {
		t.Fatalf("ArtifactRoot = %q", got.ArtifactRoot)
	}
}

func TestServerConfigHostPortUsedWhenCLIOptionsAreDefaults(t *testing.T) {
	configPath := writeServerBindConfig(t, "127.0.0.1", 11223)

	got, err := resolveHTTPOptions(httpOptions{
		ServiceName: "wowdata-server",
		ConfigPath:  configPath,
		Host:        "0.0.0.0",
		Port:        9788,
	})
	if err != nil {
		t.Fatalf("resolveHTTPOptions: %v", err)
	}

	if got.Host != "127.0.0.1" {
		t.Fatalf("Host = %q, want config host", got.Host)
	}
	if got.Port != 11223 {
		t.Fatalf("Port = %d, want config port", got.Port)
	}
}

func TestServerExplicitCLIHostPortOverrideConfig(t *testing.T) {
	configPath := writeServerBindConfig(t, "127.0.0.1", 11223)

	got, err := resolveHTTPOptions(httpOptions{
		ServiceName:  "wowdata-server",
		ConfigPath:   configPath,
		Host:         "0.0.0.0",
		Port:         9788,
		HostExplicit: true,
		PortExplicit: true,
	})
	if err != nil {
		t.Fatalf("resolveHTTPOptions: %v", err)
	}

	if got.Host != "0.0.0.0" {
		t.Fatalf("Host = %q, want explicit CLI host", got.Host)
	}
	if got.Port != 9788 {
		t.Fatalf("Port = %d, want explicit CLI port", got.Port)
	}
}

func TestServerHealthUsesSharedHealthSnapshotProvider(t *testing.T) {
	provider := &testHealthProvider{
		snapshot: health.Snapshot{
			Liveness:  health.Liveness{OK: true},
			Readiness: health.Readiness{RequiredTargetsReady: 1, RequiredTargetsTotal: 2},
			Matrix:    health.Matrix{TargetsTotal: 2, Ready: 1, Preparing: 1},
			Memory:    health.Memory{MemorySoftLimitMB: 4096, MemoryHardLimitMB: 8192},
			Storage:   health.Storage{MetadataDBBytes: 1},
			Contexts: []health.ContextStatus{{
				Label: "CN Retail",
				State: health.StateReady,
			}},
		},
	}
	handler := newHTTPHandler(httpOptions{ServiceName: "wowdata-server"}, provider)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}

	var got health.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode health: %v\n%s", err, rec.Body.String())
	}
	if got.Readiness.RequiredTargetsTotal != 2 || got.Contexts[0].Label != "CN Retail" {
		t.Fatalf("/health did not return shared snapshot: %#v", got)
	}
}

func TestServerHealthUsesMetadataBackedProviderForReadyTarget(t *testing.T) {
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	seedActiveMaterializedTable(t, metadataPath, metadata.BuildKey{
		Region:   "us",
		Product:  "wow",
		Locale:   "enUS",
		BuildKey: "active-build",
	}, "Item")
	metadataInfo, err := os.Stat(metadataPath)
	if err != nil {
		t.Fatalf("stat metadata DB: %v", err)
	}
	configPath := writeHealthConfig(t, metadataPath, "US Retail", "us", "wow", "enUS")
	handler := newHTTPHandler(httpOptions{
		ServiceName: "wowdata-server",
		ConfigPath:  configPath,
	}, newTestMetadataHealthProvider(t, configPath, metadataPath))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got health.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode health: %v\n%s", err, rec.Body.String())
	}
	if !got.Readiness.OK || got.Readiness.RequiredTargetsReady != 1 || got.Readiness.RequiredTargetsTotal != 1 {
		t.Fatalf("readiness = %#v, want 1/1 ready", got.Readiness)
	}
	if got.Matrix.Ready != 1 || got.Matrix.Preparing != 0 {
		t.Fatalf("matrix = %#v, want one ready target", got.Matrix)
	}
	if len(got.Contexts) != 1 || got.Contexts[0].State != health.StateReady || got.Contexts[0].ActiveBuild != "active-build" || !got.Contexts[0].DB2Ready {
		t.Fatalf("contexts = %#v, want active DB2-ready target", got.Contexts)
	}
	if got.Storage.MetadataDBBytes != metadataInfo.Size() {
		t.Fatalf("metadataDBBytes = %d, want %d", got.Storage.MetadataDBBytes, metadataInfo.Size())
	}
}

func TestServerHealthRequiresAllConfiguredDefaultTables(t *testing.T) {
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	seedActiveMaterializedTables(t, metadataPath, metadata.BuildKey{
		Region:   "us",
		Product:  "wow",
		Locale:   "enUS",
		BuildKey: "active-build",
	}, []string{"Item"})
	configPath := writeHealthConfigWithDefaultTables(t, metadataPath, "US Retail", "us", "wow", "enUS", []string{"Item", "Spell"})
	handler := newHTTPHandler(httpOptions{
		ServiceName: "wowdata-server",
		ConfigPath:  configPath,
	}, newTestMetadataHealthProvider(t, configPath, metadataPath))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got health.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode health: %v\n%s", err, rec.Body.String())
	}
	if got.Readiness.OK || got.Readiness.RequiredTargetsReady != 0 || got.Readiness.RequiredTargetsTotal != 1 {
		t.Fatalf("readiness = %#v, want 0/1 not ready", got.Readiness)
	}
	if got.Matrix.Ready != 0 || got.Matrix.Preparing != 1 {
		t.Fatalf("matrix = %#v, want one preparing target", got.Matrix)
	}
	if len(got.Contexts) != 1 || got.Contexts[0].State != health.StatePreparing || got.Contexts[0].DB2Ready {
		t.Fatalf("contexts = %#v, want preparing target without DB2Ready", got.Contexts)
	}
	if got.Contexts[0].PrepareCurrent != 1 || got.Contexts[0].PrepareTotal != 2 {
		t.Fatalf("prepare progress = %d/%d, want 1/2", got.Contexts[0].PrepareCurrent, got.Contexts[0].PrepareTotal)
	}
}

func TestServerHealthReadyWhenAllConfiguredDefaultTablesAreValid(t *testing.T) {
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	seedActiveMaterializedTables(t, metadataPath, metadata.BuildKey{
		Region:   "us",
		Product:  "wow",
		Locale:   "enUS",
		BuildKey: "active-build",
	}, []string{"Item", "Spell"})
	configPath := writeHealthConfigWithDefaultTables(t, metadataPath, "US Retail", "us", "wow", "enUS", []string{"Item", "Spell"})
	handler := newHTTPHandler(httpOptions{
		ServiceName: "wowdata-server",
		ConfigPath:  configPath,
	}, newTestMetadataHealthProvider(t, configPath, metadataPath))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got health.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode health: %v\n%s", err, rec.Body.String())
	}
	if !got.Readiness.OK || got.Readiness.RequiredTargetsReady != 1 || got.Readiness.RequiredTargetsTotal != 1 {
		t.Fatalf("readiness = %#v, want 1/1 ready", got.Readiness)
	}
	if got.Matrix.Ready != 1 || got.Matrix.Preparing != 0 {
		t.Fatalf("matrix = %#v, want one ready target", got.Matrix)
	}
	if len(got.Contexts) != 1 || got.Contexts[0].State != health.StateReady || !got.Contexts[0].DB2Ready {
		t.Fatalf("contexts = %#v, want ready target with DB2Ready", got.Contexts)
	}
	if got.Contexts[0].PrepareCurrent != 2 || got.Contexts[0].PrepareTotal != 2 {
		t.Fatalf("prepare progress = %d/%d, want 2/2", got.Contexts[0].PrepareCurrent, got.Contexts[0].PrepareTotal)
	}
}

func TestServerWowStatusReportsConfiguredTargetPreparingWithoutActiveMetadata(t *testing.T) {
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db, err := metadata.Open(metadataPath)
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close metadata DB: %v", err)
	}
	configPath := writeHealthConfig(t, metadataPath, "US Retail", "us", "wow", "enUS")
	handler := newHTTPHandler(httpOptions{
		ServiceName: "wowdata-server",
		ConfigPath:  configPath,
	}, newTestMetadataHealthProvider(t, configPath, metadataPath))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wow_status","arguments":{}}}`))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Result struct {
			StructuredContent struct {
				OK   bool            `json:"ok"`
				Data health.Snapshot `json:"data"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode MCP response: %v\n%s", err, rec.Body.String())
	}
	got := payload.Result.StructuredContent
	if !got.OK {
		t.Fatalf("wow_status returned error: %s", rec.Body.String())
	}
	if got.Data.Readiness.OK || got.Data.Readiness.RequiredTargetsReady != 0 || got.Data.Readiness.RequiredTargetsTotal != 1 {
		t.Fatalf("readiness = %#v, want 0/1 not ready", got.Data.Readiness)
	}
	if got.Data.Matrix.Preparing != 1 || got.Data.Matrix.Ready != 0 {
		t.Fatalf("matrix = %#v, want one preparing target", got.Data.Matrix)
	}
	if len(got.Data.Contexts) != 1 || got.Data.Contexts[0].State != health.StatePreparing {
		t.Fatalf("contexts = %#v, want preparing target", got.Data.Contexts)
	}
}

func TestServerHealthKeepsActiveNonValidBuildPreparingDespiteValidTable(t *testing.T) {
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	buildKey := metadata.BuildKey{
		Region:   "us",
		Product:  "wow",
		Locale:   "enUS",
		BuildKey: "failed-build",
	}
	db, err := metadata.Open(metadataPath)
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	ctx := context.Background()
	if err := metadata.UpsertDiscoveredBuild(ctx, db, metadata.Build{
		Key:       buildKey,
		BuildName: buildKey.BuildKey,
		State:     metadata.StateFailed,
		Active:    true,
	}); err != nil {
		t.Fatalf("upsert active failed build: %v", err)
	}
	if err := metadata.UpsertMaterializedTable(ctx, db, metadata.MaterializedTable{
		Key: metadata.TableKey{
			Region:    buildKey.Region,
			Product:   buildKey.Product,
			Locale:    buildKey.Locale,
			BuildKey:  buildKey.BuildKey,
			TableName: "Item",
		},
		DB2FileDataID:       1,
		DBDHash:             "dbd-a",
		DecoderVersion:      "decoder-1",
		MaterializerVersion: "materializer-1",
		ParquetPath:         "cache/db2/item.parquet",
		RowCount:            1,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert materialized table: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close metadata DB: %v", err)
	}
	configPath := writeHealthConfig(t, metadataPath, "US Retail", "us", "wow", "enUS")
	handler := newHTTPHandler(httpOptions{
		ServiceName: "wowdata-server",
		ConfigPath:  configPath,
	}, newTestMetadataHealthProvider(t, configPath, metadataPath))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got health.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode health: %v\n%s", err, rec.Body.String())
	}
	if got.Readiness.OK || got.Readiness.RequiredTargetsReady != 0 || got.Readiness.RequiredTargetsTotal != 1 {
		t.Fatalf("readiness = %#v, want 0/1 not ready", got.Readiness)
	}
	if got.Matrix.Ready != 0 || got.Matrix.Preparing != 1 {
		t.Fatalf("matrix = %#v, want one preparing target", got.Matrix)
	}
	if len(got.Contexts) != 1 || got.Contexts[0].State != health.StatePreparing || got.Contexts[0].DB2Ready {
		t.Fatalf("contexts = %#v, want preparing target without DB2Ready", got.Contexts)
	}
	if got.Contexts[0].ActiveBuild != "failed-build" {
		t.Fatalf("activeBuild = %q, want failed-build", got.Contexts[0].ActiveBuild)
	}
}

func TestServerMCPRouteListsTools(t *testing.T) {
	handler := newHTTPHandler(httpOptions{ServiceName: "wowdata-server"}, &testHealthProvider{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"wow_query"`) {
		t.Fatalf("/mcp tools/list missing wow_query: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "mcp_not_wired") {
		t.Fatalf("/mcp still returns placeholder response: %s", rec.Body.String())
	}
}

func TestServerMCPTrailingSlashRouteListsTools(t *testing.T) {
	handler := newHTTPHandler(httpOptions{ServiceName: "wowdata-server"}, &testHealthProvider{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"wow_query"`) {
		t.Fatalf("/mcp/ tools/list missing wow_query: %s", rec.Body.String())
	}
}

func TestServerDefaultMCPHandlerAnswersTablesFromMetadata(t *testing.T) {
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db, err := metadata.Open(metadataPath)
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	ctx := context.Background()
	if err := metadata.UpsertMaterializedTable(ctx, db, metadata.MaterializedTable{
		Key:                 metadata.TableKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1", TableName: "Item"},
		DB2FileDataID:       1,
		DBDHash:             "dbd-a",
		DecoderVersion:      "decoder-1",
		MaterializerVersion: "materializer-1",
		ParquetPath:         "cache/db2/item.parquet",
		RowCount:            1,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert Item metadata: %v", err)
	}
	if err := metadata.UpsertMaterializedTable(ctx, db, metadata.MaterializedTable{
		Key:                 metadata.TableKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2", TableName: "OldBuildOnly"},
		DB2FileDataID:       2,
		DBDHash:             "dbd-b",
		DecoderVersion:      "decoder-1",
		MaterializerVersion: "materializer-1",
		ParquetPath:         "cache/db2/old.parquet",
		RowCount:            1,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert OldBuildOnly metadata: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close metadata DB: %v", err)
	}

	handler := newHTTPHandler(httpOptions{
		ServiceName:    "wowdata-server",
		MetadataDBPath: metadataPath,
	}, &testHealthProvider{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wow_query","arguments":{"mode":"tables","region":"us","product":"wow","locale":"enUS","buildKey":"build-1"}}}`))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Result struct {
			StructuredContent struct {
				OK      bool   `json:"ok"`
				Command string `json:"command"`
				Data    struct {
					Count  int `json:"count"`
					Tables []struct {
						Name string `json:"name"`
					} `json:"tables"`
				} `json:"data"`
				Error map[string]interface{} `json:"error"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode MCP response: %v\n%s", err, rec.Body.String())
	}
	got := payload.Result.StructuredContent
	if !got.OK {
		t.Fatalf("wow_query mode=tables returned error instead of metadata catalog: %#v", got.Error)
	}
	if got.Command != "query tables" {
		t.Fatalf("command = %q, want query tables", got.Command)
	}
	if got.Data.Count != 1 || len(got.Data.Tables) != 1 || got.Data.Tables[0].Name != "Item" {
		t.Fatalf("tables payload = %#v, want only Item from requested build", got.Data)
	}
}

func TestServerDefaultMCPHandlerUsesConfigMetadataDB(t *testing.T) {
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db, err := metadata.Open(metadataPath)
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	ctx := context.Background()
	if err := metadata.UpsertDiscoveredBuild(ctx, db, metadata.Build{
		Key:       metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "active-build"},
		BuildName: "active-build",
		State:     metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert active build: %v", err)
	}
	if err := metadata.ActivateBuild(ctx, db, metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "active-build"}); err != nil {
		t.Fatalf("activate build: %v", err)
	}
	if err := metadata.UpsertMaterializedTable(ctx, db, metadata.MaterializedTable{
		Key:                 metadata.TableKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "active-build", TableName: "Item"},
		DB2FileDataID:       1,
		DBDHash:             "dbd-a",
		DecoderVersion:      "decoder-1",
		MaterializerVersion: "materializer-1",
		ParquetPath:         "cache/db2/item.parquet",
		RowCount:            1,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert Item metadata: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close metadata DB: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "http-mcp.yaml")
	configBody := "cache:\n  metadata_db: " + metadataPath + "\n"
	if err := os.WriteFile(configPath, []byte(configBody), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	handler := newHTTPHandler(httpOptions{
		ServiceName: "wowdata-server",
		ConfigPath:  configPath,
	}, &testHealthProvider{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wow_query","arguments":{"mode":"tables","region":"us","product":"wow","locale":"enUS"}}}`))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"Item"`) {
		t.Fatalf("mode=tables did not use configured metadata DB: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "query_engine_unavailable") {
		t.Fatalf("mode=tables returned unavailable despite configured metadata DB: %s", rec.Body.String())
	}
}

func TestServerProductionWiringStartsBootstrapWithLoadedConfigDefaultTables(t *testing.T) {
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	configPath := writeHealthConfigWithDefaultTables(t, metadataPath, "US Retail", "us", "wow", "enUS", []string{"Item", "Spell"})
	var gotCfgTable string
	var gotTables []string
	var gotTarget string
	called := false

	handler := newHTTPHandler(httpOptions{
		ServiceName: "wowdata-server",
		ConfigPath:  configPath,
		Bootstrap: func(ctx context.Context, cfg serverbootstrap.Config, db *sql.DB) error {
			called = true
			if db == nil {
				t.Fatal("bootstrap db is nil")
			}
			gotCfgTable = cfg.Cache.MetadataDB
			gotTables = append([]string{}, cfg.Prepare.DefaultTables...)
			if len(cfg.Prepare.Targets) == 1 {
				target := cfg.Prepare.Targets[0]
				gotTarget = target.Region + "/" + target.Product + "/" + target.Locale
			}
			return nil
		},
	}, nil)
	if closer, ok := handler.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = closer.Close() })
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("bootstrap hook was not called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if gotCfgTable != metadataPath {
		t.Fatalf("bootstrap metadata db = %q, want %q", gotCfgTable, metadataPath)
	}
	if strings.Join(gotTables, ",") != "Item,Spell" {
		t.Fatalf("bootstrap tables = %#v, want Item,Spell", gotTables)
	}
	if gotTarget != "us/wow/enUS" {
		t.Fatalf("bootstrap target = %q, want us/wow/enUS", gotTarget)
	}
}

func TestServerFileRouteServesArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "exports"), 0755); err != nil {
		t.Fatalf("create exports dir: %v", err)
	}
	want := []byte("server artifact")
	if err := os.WriteFile(filepath.Join(root, "exports", "data.json"), want, 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	handler := newHTTPHandler(httpOptions{
		ServiceName:  "wowdata-server",
		ArtifactRoot: root,
	}, &testHealthProvider{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/exports/data.json", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatalf("body = %q, want %q", rec.Body.Bytes(), want)
	}
}

type testHealthProvider struct {
	snapshot health.Snapshot
	calls    int
}

func (p *testHealthProvider) HealthSnapshot(context.Context) (health.Snapshot, error) {
	p.calls++
	return p.snapshot, nil
}

func newTestMetadataHealthProvider(t *testing.T, configPath, metadataPath string) health.Provider {
	t.Helper()
	cfg, err := loadServerConfig(configPath)
	if err != nil {
		t.Fatalf("load server config: %v", err)
	}
	provider, err := health.NewMetadataProvider(cfg, metadataPath)
	if err != nil {
		t.Fatalf("new metadata health provider: %v", err)
	}
	t.Cleanup(func() {
		if err := provider.Close(); err != nil {
			t.Fatalf("close metadata health provider: %v", err)
		}
	})
	return provider
}

func seedActiveMaterializedTable(t *testing.T, metadataPath string, buildKey metadata.BuildKey, tableName string) {
	t.Helper()
	seedActiveMaterializedTables(t, metadataPath, buildKey, []string{tableName})
}

func seedActiveMaterializedTables(t *testing.T, metadataPath string, buildKey metadata.BuildKey, tableNames []string) {
	t.Helper()
	db, err := metadata.Open(metadataPath)
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := metadata.UpsertDiscoveredBuild(ctx, db, metadata.Build{
		Key:       buildKey,
		BuildName: buildKey.BuildKey,
		State:     metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert active build: %v", err)
	}
	if err := metadata.ActivateBuild(ctx, db, buildKey); err != nil {
		t.Fatalf("activate build: %v", err)
	}
	for index, tableName := range tableNames {
		if err := metadata.UpsertMaterializedTable(ctx, db, metadata.MaterializedTable{
			Key: metadata.TableKey{
				Region:    buildKey.Region,
				Product:   buildKey.Product,
				Locale:    buildKey.Locale,
				BuildKey:  buildKey.BuildKey,
				TableName: tableName,
			},
			DB2FileDataID:       index + 1,
			DBDHash:             "dbd-a",
			DecoderVersion:      "decoder-1",
			MaterializerVersion: "materializer-1",
			ParquetPath:         "cache/db2/" + strings.ToLower(tableName) + ".parquet",
			RowCount:            1,
			State:               metadata.StateValid,
		}); err != nil {
			t.Fatalf("upsert materialized table %s: %v", tableName, err)
		}
	}
}

func writeServerBindConfig(t *testing.T, host string, port int) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "http-mcp.yaml")
	body := strings.Join([]string{
		"server:",
		"  host: " + host,
		"  port: " + strconv.Itoa(port),
		"cache:",
		"  metadata_db: " + filepath.ToSlash(filepath.Join(t.TempDir(), "metadata.sqlite")),
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(body), 0644); err != nil {
		t.Fatalf("write server bind config: %v", err)
	}
	return configPath
}

func writeHealthConfig(t *testing.T, metadataPath, label, region, product, locale string) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "http-mcp.yaml")
	body := strings.Join([]string{
		"cache:",
		"  metadata_db: " + metadataPath,
		"prepare:",
		"  targets:",
		"    - label: " + label,
		"      region: " + region,
		"      product: " + product,
		"      locale: " + locale,
		"  default_tables:",
		"    - Item",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(body), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

func writeHealthConfigWithDefaultTables(t *testing.T, metadataPath, label, region, product, locale string, defaultTables []string) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "http-mcp.yaml")
	lines := []string{
		"cache:",
		"  metadata_db: " + metadataPath,
		"prepare:",
		"  targets:",
		"    - label: " + label,
		"      region: " + region,
		"      product: " + product,
		"      locale: " + locale,
		"  default_tables:",
	}
	for _, tableName := range defaultTables {
		lines = append(lines, "    - "+tableName)
	}
	lines = append(lines, "")
	if err := os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}
