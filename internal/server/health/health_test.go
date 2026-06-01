package health

import (
	"encoding/json"
	"testing"
)

func TestHealthReportsLivenessReadinessMatrixMemoryStorageAndErrors(t *testing.T) {
	snapshot := BuildSnapshot(Input{
		Targets: []TargetInput{
			{
				Label:          "CN Retail",
				Region:         "cn",
				Product:        "wow",
				Locale:         "zhCN",
				State:          StateReady,
				ActiveBuild:    "11.2.0.12345",
				DB2Ready:       true,
				ListfileReady:  true,
				CASCReady:      true,
				PrepareCurrent: 10,
				PrepareTotal:   10,
			},
			{
				Label:          "US PTR",
				Region:         "us",
				Product:        "wowt",
				Locale:         "enUS",
				State:          StatePreparing,
				Error:          "materializing Spell",
				PrepareCurrent: 3,
				PrepareTotal:   10,
			},
		},
		Memory: Memory{
			MemorySoftLimitMB: 4096,
			MemoryHardLimitMB: 8192,
		},
		Storage: Storage{
			MetadataDBBytes: 1,
			RawCacheBytes:   2,
			DB2Bytes:        3,
			ArtifactBytes:   4,
		},
		RecentErrors: []string{"US PTR: materializing Spell"},
	})

	assertJSONField(t, snapshot, "liveness.ok", true)
	assertJSONField(t, snapshot, "readiness.ok", false)
	assertJSONField(t, snapshot, "readiness.requiredTargetsReady", float64(1))
	assertJSONField(t, snapshot, "readiness.requiredTargetsTotal", float64(2))
	assertJSONField(t, snapshot, "matrix.targetsTotal", float64(2))
	assertJSONField(t, snapshot, "matrix.ready", float64(1))
	assertJSONField(t, snapshot, "matrix.preparing", float64(1))
	assertJSONField(t, snapshot, "memory.memorySoftLimitMB", float64(4096))
	assertJSONField(t, snapshot, "memory.memoryHardLimitMB", float64(8192))
	assertJSONField(t, snapshot, "storage.metadataDBBytes", float64(1))
	assertJSONField(t, snapshot, "contexts.0.label", "CN Retail")
	assertJSONField(t, snapshot, "contexts.0.state", "ready")
	assertJSONField(t, snapshot, "contexts.1.error", "materializing Spell")

	if len(snapshot.RecentErrors) != 1 {
		t.Fatalf("RecentErrors = %#v, want one error", snapshot.RecentErrors)
	}
}

func TestNoBuildDoesNotBlockReadinessUnlessStrict(t *testing.T) {
	notStrict := BuildSnapshot(Input{Targets: []TargetInput{
		{Label: "CN Retail", State: StateReady},
		{Label: "TW Unsupported", State: StateNoBuild},
	}})
	if !notStrict.Readiness.OK {
		t.Fatalf("readiness with non-strict no_build = false, want true: %#v", notStrict.Readiness)
	}
	if notStrict.Readiness.RequiredTargetsTotal != 1 || notStrict.Readiness.RequiredTargetsReady != 1 {
		t.Fatalf("non-strict no_build required counts = %#v, want 1/1", notStrict.Readiness)
	}

	strict := BuildSnapshot(Input{Targets: []TargetInput{
		{Label: "CN Retail", State: StateReady},
		{Label: "TW Unsupported", State: StateNoBuild, Strict: true},
	}})
	if strict.Readiness.OK {
		t.Fatalf("readiness with strict no_build = true, want false: %#v", strict.Readiness)
	}
	if strict.Readiness.RequiredTargetsTotal != 2 || strict.Readiness.RequiredTargetsReady != 1 {
		t.Fatalf("strict no_build required counts = %#v, want 1/2", strict.Readiness)
	}
}

func assertJSONField(t *testing.T, snapshot Snapshot, path string, want interface{}) {
	t.Helper()
	body, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	var doc interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	got := lookupJSONPath(t, doc, path)
	if got != want {
		t.Fatalf("%s = %#v, want %#v\njson: %s", path, got, want, body)
	}
}

func lookupJSONPath(t *testing.T, doc interface{}, path string) interface{} {
	t.Helper()
	current := doc
	for _, part := range splitPath(path) {
		switch node := current.(type) {
		case map[string]interface{}:
			current = node[part]
		case []interface{}:
			if part != "0" && part != "1" {
				t.Fatalf("unsupported test path index %q in %q", part, path)
			}
			index := 0
			if part == "1" {
				index = 1
			}
			if index >= len(node) {
				t.Fatalf("path %q index %d out of range", path, index)
			}
			current = node[index]
		default:
			t.Fatalf("path %q reached non-container %#v", path, current)
		}
	}
	return current
}

func splitPath(path string) []string {
	var parts []string
	start := 0
	for i, r := range path {
		if r == '.' {
			parts = append(parts, path[start:i])
			start = i + 1
		}
	}
	return append(parts, path[start:])
}
