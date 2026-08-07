package hotfix

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestHotfixPackageDoesNotImportSQL(t *testing.T) {
	pkgs, err := parser.ParseDir(token.NewFileSet(), ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, imp := range file.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				if strings.Contains(path, "/sqlquery") {
					t.Fatalf("%s imports %s", name, path)
				}
			}
		}
	}
}
