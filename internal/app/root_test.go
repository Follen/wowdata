package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestResponseEnvelopeSuccess(t *testing.T) {
	resp := NewSuccessResponse("wowdata test", map[string]any{"value": 42})

	if !resp.OK {
		t.Fatalf("expected OK response")
	}
	if resp.Command != "wowdata test" {
		t.Fatalf("command = %q", resp.Command)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %#v", resp.Error)
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	for _, key := range []string{"ok", "command", "data", "warnings"} {
		if _, exists := decoded[key]; !exists {
			t.Fatalf("missing envelope key %q in %#v", key, decoded)
		}
	}
}

func TestResponseEnvelopeError(t *testing.T) {
	resp := NewErrorResponse("wowdata bad", "service_unavailable", "no runtime service was attached for this command")

	if resp.OK {
		t.Fatalf("expected non-OK response")
	}
	if resp.Error == nil {
		t.Fatalf("expected error payload")
	}
	if resp.Error.Code != "service_unavailable" {
		t.Fatalf("error code = %q", resp.Error.Code)
	}
}

func TestResponseWriterErrorWritesJSONAndReturnsCommandError(t *testing.T) {
	var stdout bytes.Buffer
	err := NewResponseWriter(&stdout).Error("wowdata bad", "bad_input", "bad input")
	if !IsCommandError(err) {
		t.Fatalf("error = %v, want CommandError", err)
	}

	var resp Response
	if decodeErr := json.Unmarshal(stdout.Bytes(), &resp); decodeErr != nil {
		t.Fatalf("decode stdout: %v", decodeErr)
	}
	if resp.OK || resp.Error == nil || resp.Error.Code != "bad_input" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRootHelp(t *testing.T) {
	stdout, stderr, err := executeCommand(t, "--help")
	if err != nil {
		t.Fatalf("help returned error: %v stderr=%s", err, stderr)
	}
	for _, want := range []string{
		"wowdata",
		"warmup",
		"db2",
		"golden",
		"Examples:",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("root help missing %q:\n%s", want, stdout)
		}
	}
}

func TestCascInfoHelp(t *testing.T) {
	stdout, stderr, err := executeCommand(t, "casc", "info", "--help")
	if err != nil {
		t.Fatalf("casc info --help returned error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Show current build and cache state") {
		t.Fatalf("casc info help missing description:\n%s", stdout)
	}
}

func TestCascProductsHelp(t *testing.T) {
	stdout, stderr, err := executeCommand(t, "casc", "products", "--help")
	if err != nil {
		t.Fatalf("casc products --help returned error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "List available products and builds") {
		t.Fatalf("casc products help missing description:\n%s", stdout)
	}
}

func TestWarmupHelp(t *testing.T) {
	stdout, stderr, err := executeCommand(t, "warmup", "--help")
	if err != nil {
		t.Fatalf("warmup --help returned error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Prepare a complete local or remote WoW data target") {
		t.Fatalf("warmup help missing description:\n%s", stdout)
	}
	for _, want := range []string{"--tables", "--listfile", "--listfile-format", "--dbd-manifest", "--locale", "--cache"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("warmup help missing %s flag:\n%s", want, stdout)
		}
	}
}

func TestFileExportHelpIncludesFilename(t *testing.T) {
	stdout, stderr, err := executeCommand(t, "file", "export", "--help")
	if err != nil {
		t.Fatalf("file export --help returned error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "--filename") {
		t.Fatalf("file export help should include --filename:\n%s", stdout)
	}
}

func TestCreatureDisplayHelpIncludesFileDataID(t *testing.T) {
	stdout, stderr, err := executeCommand(t, "creature", "display", "--help")
	if err != nil {
		t.Fatalf("creature display --help returned error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "--file-data-id") {
		t.Fatalf("creature display help should include --file-data-id:\n%s", stdout)
	}
}

func TestCascInfoReturnsServiceUnavailableWithoutInjectedRuntime(t *testing.T) {
	stdout, stderr, err := executeCommand(t, "casc", "info")
	if !IsCommandError(err) {
		t.Fatalf("casc info error = %v, want CommandError; stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": false`) {
		t.Fatalf("expected ok=false:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"service_unavailable"`) {
		t.Fatalf("expected service_unavailable:\n%s", stdout)
	}
}

func TestHandlerInjectionSeam(t *testing.T) {
	// Create a Service with a custom handler for casc info
	svc := &Service{
		Casc: func(cmd *cobra.Command, args []string) error {
			resp := NewSuccessResponse("casc", map[string]any{"injected": true})
			return writeJSON(cmd.OutOrStdout(), resp)
		},
	}

	cmd := NewRootCommandWithService(svc)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"casc", "info"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("casc info returned error: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"ok": true`) {
		t.Fatalf("expected ok=true from injected handler:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"injected": true`) {
		t.Fatalf("expected injected data from injected handler:\n%s", stdout.String())
	}
}

func TestAllPlannedCommandHelp(t *testing.T) {
	commands := [][]string{
		{"warmup"},
		{"db2"}, {"db2", "schema"}, {"db2", "rows"}, {"db2", "search"}, {"db2", "foreign-key"}, {"db2", "stream"},
		{"spell"}, {"spell", "info"}, {"spell", "auras"}, {"spell", "summons"},
		{"encounter"}, {"encounter", "get"},
		{"file"}, {"file", "lookup"}, {"file", "search"}, {"file", "extension"}, {"file", "get"}, {"file", "exists"}, {"file", "encoding"}, {"file", "export"},
		{"icon"}, {"icon", "export"},
		{"casc"}, {"casc", "info"}, {"casc", "products"}, {"casc", "diagnose"},
		{"item"}, {"item", "get"}, {"item", "models"}, {"item", "geosets"}, {"item", "textures"},
		{"creature"}, {"creature", "display"}, {"creature", "model"},
		{"decor"}, {"decor", "list"}, {"decor", "get"},
		{"video"}, {"video", "demux"},
		{"profile"}, {"profile", "list"}, {"profile", "show"}, {"profile", "set"}, {"profile", "remove"},
		{"cache"}, {"cache", "status"}, {"cache", "verify"}, {"cache", "prune"}, {"cache", "clear"}, {"cache", "config"},
		{"doctor"}, {"update"}, {"uninstall"},
		{"golden"}, {"golden", "capture"}, {"golden", "compare"},
	}

	for _, parts := range commands {
		args := append(append([]string{}, parts...), "--help")
		stdout, stderr, err := executeCommand(t, args...)
		if err != nil {
			t.Fatalf("%v --help returned error: %v stderr=%s", parts, err, stderr)
		}
		if !strings.Contains(stdout, "Usage:") {
			t.Fatalf("%v --help missing usage:\n%s", parts, stdout)
		}
		if !strings.Contains(stdout, "Examples:") {
			t.Fatalf("%v --help missing examples:\n%s", parts, stdout)
		}
	}
}
