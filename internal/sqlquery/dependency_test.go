package sqlquery

import (
	goparser "go/parser"
	gotoken "go/token"
	"strconv"
	"strings"
	"testing"
)

func TestSQLPackageDoesNotImportHotfix(t *testing.T) {
	pkgs, err := goparser.ParseDir(gotoken.NewFileSet(), ".", nil, goparser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, imp := range file.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				if strings.Contains(path, "/hotfix") {
					t.Fatalf("%s imports %s", name, path)
				}
			}
		}
	}
}
