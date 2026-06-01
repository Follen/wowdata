package cache

import (
	"path/filepath"
	"testing"
)

func TestDB2ParquetPath(t *testing.T) {
	got := filepath.ToSlash(DB2ParquetPath("root", "cn", "wow", "abcd", "zhCN", "SpellName"))
	want := "root/db2/cn/wow/abcd/zhCN/SpellName.parquet"
	if got != want {
		t.Fatalf("DB2ParquetPath = %q, want %q", got, want)
	}
}

func TestRawCASCPath(t *testing.T) {
	got := filepath.ToSlash(RawCASCPath("root", "cn", "wow", "abcd"))
	want := "root/raw/casc/cn/wow/abcd"
	if got != want {
		t.Fatalf("RawCASCPath = %q, want %q", got, want)
	}
}

func TestEnsureUnderRootRejectsTraversal(t *testing.T) {
	if _, err := EnsureUnderRoot("root", "../evil"); err == nil {
		t.Fatal("EnsureUnderRoot accepted traversal")
	}
}

func TestEnsureUnderRootRejectsEmptyRoot(t *testing.T) {
	if _, err := EnsureUnderRoot("", "child"); err == nil {
		t.Fatal("EnsureUnderRoot accepted empty root")
	}
}

func TestEnsureUnderRootRejectsAbsolutePathOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "evil")
	if _, err := EnsureUnderRoot(root, outside); err == nil {
		t.Fatal("EnsureUnderRoot accepted absolute path outside root")
	}
}
