package app

import (
	"strings"
	"testing"

	"wowdata/internal/casc"
	"wowdata/internal/listfile"
)

func TestIncompleteDB2RowsDoesNotReturnSuccess(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{DB2: NewDB2Handler()}, "query", "rows", "SpellName", "--id", "123")
	if err != nil {
		t.Fatalf("db2 rows returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": false`) {
		t.Fatalf("db2 rows without runtime must fail truthfully:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"not_ready"`) {
		t.Fatalf("db2 rows should report not_ready:\n%s", stdout)
	}
}

func TestIncompleteDB2SchemaDoesNotReturnSuccess(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{DB2: NewDB2Handler()}, "query", "schema", "SpellName")
	if err != nil {
		t.Fatalf("db2 schema returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": false`) {
		t.Fatalf("db2 schema without runtime must fail truthfully:\n%s", stdout)
	}
}

func TestIncompleteFileExportDoesNotReturnSuccess(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{File: NewFileHandler(listfile.New(), casc.NewFileService())}, "file", "export", "--file-data-id", "123", "--output", "out.bin")
	if err != nil {
		t.Fatalf("file export returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": false`) {
		t.Fatalf("file export without runtime must fail truthfully:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"not_ready"`) {
		t.Fatalf("file export should report not_ready:\n%s", stdout)
	}
}

func TestIncompleteFileLookupDoesNotReturnSuccess(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{File: NewFileHandler(nil, nil)}, "file", "lookup", "--file-data-id", "456")
	if err != nil {
		t.Fatalf("file lookup returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": false`) {
		t.Fatalf("file lookup without runtime must fail truthfully:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"not_ready"`) {
		t.Fatalf("file lookup should report not_ready:\n%s", stdout)
	}
	if !strings.Contains(stdout, `CASC 未就绪，请先调用 wow_warmup`) {
		t.Fatalf("file lookup should preserve warmup error message:\n%s", stdout)
	}
}

func TestIncompleteIconExportDoesNotReturnSuccess(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{Icon: NewIconHandler()}, "icon", "export", "--file-data-id", "123", "--output", "icon.png")
	if err != nil {
		t.Fatalf("icon export returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": false`) {
		t.Fatalf("icon export without runtime must fail truthfully:\n%s", stdout)
	}
}

func TestIncompleteGoldenCompareDoesNotReturnSuccess(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{Golden: NewGoldenHandler()}, "golden", "compare", "--fixture", "fixtures/golden/missing.json")
	if err != nil {
		t.Fatalf("golden compare returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": false`) {
		t.Fatalf("golden compare for missing fixture must fail truthfully:\n%s", stdout)
	}
}
