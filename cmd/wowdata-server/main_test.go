package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"wowdata/internal/server/health"
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
	if got.ArtifactRoot != "/srv/wowdata/artifacts" {
		t.Fatalf("ArtifactRoot = %q", got.ArtifactRoot)
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
