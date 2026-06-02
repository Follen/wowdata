package casc

import "testing"

func TestSourceKindValidation(t *testing.T) {
	for _, kind := range []SourceKind{SourceLocal, SourceRemote} {
		if !kind.Valid() {
			t.Fatalf("expected %s to be valid", kind)
		}
	}
	if SourceKind("bad").Valid() {
		t.Fatalf("bad source kind should be invalid")
	}
}

func TestCachePaths(t *testing.T) {
	paths := NewCachePaths("cache", "wow-123")
	if paths.Root != "cache/casc/wow-123" {
		t.Fatalf("root = %q", paths.Root)
	}
}
