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
