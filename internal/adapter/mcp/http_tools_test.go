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

func TestHTTPDB2HandlerInvokesEnsureTableBeforePlaceholderResult(t *testing.T) {
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

	result, err := tool.Handler(context.Background(), json.RawMessage(`{"fileDataID":134400}`))
	if err != nil {
		t.Fatalf("wow_icon handler: %v", err)
	}
	if linker.reserveCalls != 1 {
		t.Fatalf("ReserveArtifact calls = %d, want 1", linker.reserveCalls)
	}
	if linker.category != "icons" || linker.filename != "134400.png" || linker.mimeType != "image/png" {
		t.Fatalf("ReserveArtifact args = %q %q %q", linker.category, linker.filename, linker.mimeType)
	}
	data := result.(map[string]interface{})["data"].(map[string]interface{})
	if data["uri"] != "https://mcp.example.com/files/icons/134400.png" || data["sha256"] != "abc123" {
		t.Fatalf("icon artifact mapping mismatch: %#v", data)
	}
}

type fakeHTTPService struct {
	status      httpservice.Status
	statusCalls int
	ensureErr   error
	ensureCalls int
	lastContext httpservice.RequestContext
	lastTable   string
}

func (f *fakeHTTPService) Status() httpservice.Status {
	f.statusCalls++
	return f.status
}

func (f *fakeHTTPService) EnsureTable(ctx context.Context, rc httpservice.RequestContext, table string) error {
	f.ensureCalls++
	f.lastContext = rc
	f.lastTable = table
	return f.ensureErr
}

type fakeArtifactLinker struct {
	link         ArtifactLink
	reserveCalls int
	category     string
	filename     string
	mimeType     string
}

func (f *fakeArtifactLinker) ReserveArtifact(category, filename, mimeType string) (string, ArtifactLink, error) {
	f.reserveCalls++
	f.category = category
	f.filename = filename
	f.mimeType = mimeType
	return f.link.Path, f.link, nil
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
