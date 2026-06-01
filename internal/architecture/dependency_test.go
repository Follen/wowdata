package architecture

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

func TestServerDoesNotImportLocalPackages(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "./internal/server/...")
	cmd.Dir = "../.."
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list server deps: %v\n%s", err, out.String())
	}
	for _, dep := range strings.Fields(out.String()) {
		if strings.Contains(dep, "/internal/local/") || strings.HasSuffix(dep, "/internal/app") {
			t.Fatalf("server dependency imports local package: %s", dep)
		}
	}
}

func TestLocalDoesNotImportServerPackages(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "./cmd/wowdata", "./internal/app/...", "./internal/adapter/mcp/...")
	cmd.Dir = "../.."
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list local deps: %v\n%s", err, out.String())
	}
	for _, dep := range strings.Fields(out.String()) {
		if strings.Contains(dep, "/internal/server/") {
			t.Fatalf("local dependency imports server package: %s", dep)
		}
	}
}
