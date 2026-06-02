package architecture

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerDoesNotImportLocalPackages(t *testing.T) {
	cmd := exec.Command(goToolPath(), "list", "-deps", "./cmd/wowdata-server", "./internal/server/...")
	cmd.Dir = "../.."
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list server deps: %v\n%s", err, out.String())
	}
	for _, dep := range strings.Fields(out.String()) {
		if strings.Contains(dep, "/internal/local/") || strings.HasSuffix(dep, "/internal/app") || strings.HasSuffix(dep, "/internal/runtime") {
			t.Fatalf("server dependency imports local package: %s", dep)
		}
	}
}

func TestRuntimePackageRootsMatchServerArchitecture(t *testing.T) {
	cmd := exec.Command(goToolPath(), "list", "./internal/shared/...", "./internal/local/runtime", "./internal/server/runtime")
	cmd.Dir = "../.."
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list required runtime package roots: %v\n%s", err, out.String())
	}
}

func TestServerBootstrapDoesNotUseFullTableDB2RowMaterialization(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "server", "bootstrap", "bootstrap.go"))
	if err != nil {
		t.Fatalf("read server bootstrap: %v", err)
	}
	if strings.Contains(string(source), ".GetAllRows(") {
		t.Fatal("server bootstrap must stream DB2 rows instead of calling GetAllRows for full-table materialization")
	}
}

func TestLocalDoesNotImportServerPackages(t *testing.T) {
	cmd := exec.Command(goToolPath(), "list", "-deps", "./cmd/wowdata", "./internal/app/...", "./internal/adapter/mcp/...")
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

func goToolPath() string {
	if path := os.Getenv("WOWDATA_GO"); path != "" {
		return path
	}
	return `C:\Program Files\Go\bin\go.exe`
}
