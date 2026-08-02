package app

import (
	"strings"
	"testing"

	"wowdata/internal/casc"
	"wowdata/internal/listfile"
)

func TestIncompleteDB2RowsDoesNotReturnSuccess(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{DB2: NewDB2Handler()}, "db2", "rows", "SpellName", "--id", "123")
	requireCommandError(t, err, "not_ready", stderr)
	if !strings.Contains(stdout, `"ok": false`) {
		t.Fatalf("db2 rows without runtime must fail truthfully:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"not_ready"`) {
		t.Fatalf("db2 rows should report not_ready:\n%s", stdout)
	}
}

func TestIncompleteDB2SchemaDoesNotReturnSuccess(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{DB2: NewDB2Handler()}, "db2", "schema", "SpellName")
	requireCommandError(t, err, "not_ready", stderr)
	if !strings.Contains(stdout, `"ok": false`) {
		t.Fatalf("db2 schema without runtime must fail truthfully:\n%s", stdout)
	}
}

func TestIncompleteFileExportDoesNotReturnSuccess(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{File: NewFileHandler(listfile.New(), casc.NewFileService())}, "file", "export", "--file-data-id", "123", "--output", "out.bin")
	requireCommandError(t, err, "not_ready", stderr)
	if !strings.Contains(stdout, `"ok": false`) {
		t.Fatalf("file export without runtime must fail truthfully:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"not_ready"`) {
		t.Fatalf("file export should report not_ready:\n%s", stdout)
	}
}

func TestIncompleteFileLookupDoesNotReturnSuccess(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{File: NewFileHandler(nil, nil)}, "file", "lookup", "--file-data-id", "456")
	requireCommandError(t, err, "not_ready", stderr)
	if !strings.Contains(stdout, `"ok": false`) {
		t.Fatalf("file lookup without runtime must fail truthfully:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"not_ready"`) {
		t.Fatalf("file lookup should report not_ready:\n%s", stdout)
	}
	if !strings.Contains(stdout, `CASC 未就绪，请提供完整目标或检查准备错误`) {
		t.Fatalf("file lookup should preserve warmup error message:\n%s", stdout)
	}
}

func TestIncompleteIconExportDoesNotReturnSuccess(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{Icon: NewIconHandler()}, "icon", "export", "--file-data-id", "123", "--output", "icon.png")
	requireCommandError(t, err, "not_ready", stderr)
	if !strings.Contains(stdout, `"ok": false`) {
		t.Fatalf("icon export without runtime must fail truthfully:\n%s", stdout)
	}
}

func TestIncompleteGoldenCompareDoesNotReturnSuccess(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{Golden: NewGoldenHandler()}, "golden", "compare", "--fixture", "fixtures/golden/missing.json")
	requireCommandError(t, err, "missing_argument", stderr)
	if !strings.Contains(stdout, `"ok": false`) {
		t.Fatalf("golden compare for missing fixture must fail truthfully:\n%s", stdout)
	}
}
