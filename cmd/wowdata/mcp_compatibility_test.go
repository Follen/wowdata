package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wowdata/internal/config"
	httpservice "wowdata/internal/service/http"
)

func TestStdioMCPToolNamesStayStable(t *testing.T) {
	rt := NewRuntime()
	server := newMCPServerForRuntime(rt)
	names := map[string]bool{}
	for _, name := range server.ToolNamesForTest() {
		names[name] = true
	}
	want := []string{
		"wow_warmup",
		"wow_casc",
		"wow_db2",
		"wow_file",
		"wow_icon",
		"wow_spell",
		"wow_encounter",
		"wow_item",
		"wow_creature",
		"wow_decor",
		"wow_video",
	}
	if len(names) != len(want) {
		t.Fatalf("tool count = %d, want %d: %#v", len(names), len(want), names)
	}
	for _, name := range want {
		if !names[name] {
			t.Fatalf("tool %q missing; all=%#v", name, names)
		}
	}
}

func TestHTTPHelpListsCodexClaudeAndCCSwitch(t *testing.T) {
	help := mcpHelpHTML("http://211.154.18.253:11223")
	for _, want := range []string{
		"codex mcp add wowdata --url http://211.154.18.253:11223/mcp",
		"claude mcp add --transport http wowdata http://211.154.18.253:11223/mcp",
		"cc-switch",
		"wow_builds",
		"wow_status",
		"wow_db2",
		"wow_icon",
		"Local stdio fallback",
		"Admin tools",
		"Artifact download behavior",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("mcp help missing %q:\n%s", want, help)
		}
	}
}

func TestHTTPMCPBusinessToolsUseRuntimeHandlersInsteadOfCapabilityPlaceholders(t *testing.T) {
	handlerFor := httpRuntimeCLIHandler(NewRuntime(), artifactConfig{}, httpservice.ContextDefaults{
		Region:  "cn",
		Product: "wow",
		Locale:  "zhCN",
	})
	for _, name := range []string{"wow_item", "wow_spell", "wow_file", "wow_icon", "wow_creature", "wow_encounter", "wow_decor", "wow_video"} {
		if handlerFor(name) == nil {
			t.Fatalf("%s is not wired to an HTTP runtime handler", name)
		}
	}
}

func TestHTTPHealthReportsConfiguredCacheRoot(t *testing.T) {
	rt := NewRuntime()
	rt.CacheRoot = filepath.Join(t.TempDir(), "runtime-cache")
	cacheRoot := filepath.Join(t.TempDir(), "http-cache")
	mux := http.NewServeMux()
	registerMCPHTTPHandlers(mux, newMCPServerForRuntime(rt), "https://mcp.lychee-addon.online:9443", cacheRoot, "")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode health: %v; body=%s", err, rec.Body.String())
	}
	got, _ := payload["cacheRoot"].(string)
	if got != filepath.ToSlash(cacheRoot) {
		t.Fatalf("cacheRoot = %q, want %q; payload=%#v", got, filepath.ToSlash(cacheRoot), payload)
	}
}

func TestHTTPFilesServesConfiguredArtifactRoot(t *testing.T) {
	root := t.TempDir()
	artifactPath := filepath.Join(root, "icons", "134400.png")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("icon bytes"), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	mux := http.NewServeMux()
	registerMCPHTTPHandlers(mux, newMCPServerForRuntime(NewRuntime()), "http://211.154.18.253:11223", t.TempDir(), root)

	req := httptest.NewRequest(http.MethodGet, "/files/icons/134400.png", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "icon bytes" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestHTTPFilesRejectsArtifactTraversal(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("private-content"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	mux := http.NewServeMux()
	registerMCPHTTPHandlers(mux, newMCPServerForRuntime(NewRuntime()), "http://211.154.18.253:11223", t.TempDir(), root)

	req := httptest.NewRequest(http.MethodGet, "/files/../"+filepath.Base(outside), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "private-content") {
		t.Fatalf("traversal should not be served: status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestMCPCommandHasStdioAndHTTPSubcommands(t *testing.T) {
	rt := NewRuntime()
	root := newRootCommandForRuntime(rt)
	mcp, _, err := root.Find([]string{"mcp"})
	if err != nil {
		t.Fatalf("find mcp: %v", err)
	}
	for _, name := range []string{"stdio", "http"} {
		if child, _, err := mcp.Find([]string{name}); err != nil || child.Name() != name {
			t.Fatalf("mcp subcommand %q missing: child=%v err=%v", name, child, err)
		}
	}
}

func TestHTTPMCPToolNamesExcludeWarmupAndIncludeStatusBuilds(t *testing.T) {
	svc := httpservice.NewService(config.DefaultHTTPConfig(), nil)
	server := newMCPHTTPServerForService(svc, NewRuntime(), artifactConfig{})
	var stdin, stdout bytes.Buffer
	writeMCPFrameForTest(&stdin, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))

	if err := server.Serve(context.Background(), &stdin, &stdout); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	out := stdout.String()
	if bytes.Contains(stdout.Bytes(), []byte(`"name":"wow_warmup"`)) {
		t.Fatalf("HTTP MCP tools/list should not include wow_warmup:\n%s", out)
	}
	for _, want := range []string{`"name":"wow_status"`, `"name":"wow_builds"`} {
		if !bytes.Contains(stdout.Bytes(), []byte(want)) {
			t.Fatalf("HTTP MCP tools/list missing %s:\n%s", want, out)
		}
	}
}

func TestNewHTTPServiceRuntimeWiresMaterializerAndMetadataDB(t *testing.T) {
	cfg := config.DefaultHTTPConfig()
	cfg.Cache.Root = t.TempDir()
	cfg.Cache.MetadataDB = filepath.Join(t.TempDir(), "metadata.sqlite")
	cfg.Cache.DuckDBPath = filepath.Join(t.TempDir(), "wowdata.duckdb")
	rt := NewRuntime()

	svc, closeFn, err := NewHTTPServiceRuntime(cfg, rt)
	if err != nil {
		t.Fatalf("NewHTTPServiceRuntime: %v", err)
	}
	defer closeFn()

	if svc == nil {
		t.Fatal("service is nil")
	}
	if !svc.HasMaterializerForTest() {
		t.Fatal("HTTP service materializer is not wired")
	}
	if !svc.HasMetadataDBForTest() {
		t.Fatal("HTTP service metadata DB is not wired")
	}
}
