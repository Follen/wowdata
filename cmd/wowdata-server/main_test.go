package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
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
}
