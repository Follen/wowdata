package golden

import "testing"

func TestFixturePathIsDeterministic(t *testing.T) {
	path := FixturePath("db2", "spellname-123")
	want := "fixtures/golden/db2/spellname-123.json"
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestComparePlaceholder(t *testing.T) {
	result := CompareResult{
		Fixture: "fixtures/golden/db2/spellname-123.json",
		Equal:   false,
		Reason:  "not_implemented",
	}
	if result.Equal {
		t.Fatalf("placeholder compare should not report equality")
	}
	if result.Reason != "not_implemented" {
		t.Fatalf("reason = %q", result.Reason)
	}
}
