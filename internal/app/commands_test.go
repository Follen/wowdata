package app

import "testing"

func TestBuildCommandAppliesNumericDefaults(t *testing.T) {
	cmd := buildCommand(commandSpec{
		use: "test",
		flags: []cmdFlag{
			{name: "boss", typ: "int", dflt: "1"},
			{name: "file-data-id", typ: "uint32", dflt: "42"},
			{name: "limit", typ: "int"},
		},
	}, nil)

	boss, err := cmd.Flags().GetInt("boss")
	if err != nil || boss != 1 {
		t.Fatalf("boss default = %d, %v; want 1", boss, err)
	}
	fileDataID, err := cmd.Flags().GetUint32("file-data-id")
	if err != nil || fileDataID != 42 {
		t.Fatalf("file-data-id default = %d, %v; want 42", fileDataID, err)
	}
	limit, err := cmd.Flags().GetInt("limit")
	if err != nil || limit != 0 {
		t.Fatalf("empty int default = %d, %v; want 0", limit, err)
	}
}
