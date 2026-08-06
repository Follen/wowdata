package golden

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stateCaptureJSON(t *testing.T, stdout string) []byte {
	t.Helper()
	value, err := json.Marshal(map[string]interface{}{"exitCode": 0, "stdout": stdout, "stderr": ""})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestCacheStateComparisonAllowsOnlyConsistentMetadataSizeDrift(t *testing.T) {
	expected := stateCaptureJSON(t, `{"ok":true,"data":{"path":"C:/capture/cache","sizeBytes":206,"maxBytes":1000,"format":{"schema":"wowdata.cache-format.v1","initializedAtUtc":"2026-08-05T01:00:00Z","clearedPath":"C:/capture/cache"},"usage":{"totalBytes":206,"derivedMetadataBytes":206,"payloadBytes":0,"resumeBytes":0,"uniquePayloadBytes":0,"duplicatePayloadBytes":0}}}`)
	actual := stateCaptureJSON(t, `{"ok":true,"data":{"path":"D:/run/cache","sizeBytes":197,"maxBytes":1000,"format":{"schema":"wowdata.cache-format.v1","initializedAtUtc":"2026-08-05T02:00:00Z","clearedPath":"D:/run/cache"},"usage":{"totalBytes":197,"derivedMetadataBytes":197,"payloadBytes":0,"resumeBytes":0,"uniquePayloadBytes":0,"duplicatePayloadBytes":0}}}`)
	result, err := CompareJSONWithMode(expected, actual, ComparisonCacheStateV1)
	if err != nil || !result.Equal {
		t.Fatalf("consistent cache state mismatch: result=%#v err=%v", result, err)
	}
	bad := stateCaptureJSON(t, `{"ok":true,"data":{"path":"D:/run/cache","sizeBytes":197,"maxBytes":1000,"format":{"schema":"wowdata.cache-format.v1","initializedAtUtc":"2026-08-05T02:00:00Z","clearedPath":"D:/run/cache"},"usage":{"totalBytes":198,"derivedMetadataBytes":197,"payloadBytes":0,"resumeBytes":0,"uniquePayloadBytes":0,"duplicatePayloadBytes":0}}}`)
	result, err = CompareJSONWithMode(expected, bad, ComparisonCacheStateV1)
	if err != nil || result.Equal || !strings.Contains(result.Reason, "inconsistent") {
		t.Fatalf("invalid cache accounting result=%#v err=%v", result, err)
	}
}

func TestProfileStateComparisonNormalizesOnlyValidUpdatedAt(t *testing.T) {
	expected := stateCaptureJSON(t, `{"ok":true,"data":{"name":"benchmark","target":{"build":"1"},"updatedAt":"2026-08-05T01:00:00Z"}}`)
	actual := stateCaptureJSON(t, `{"ok":true,"data":{"name":"benchmark","target":{"build":"1"},"updatedAt":"2026-08-05T02:00:00Z"}}`)
	result, err := CompareJSONWithMode(expected, actual, ComparisonProfileStateV1)
	if err != nil || !result.Equal {
		t.Fatalf("profile timestamp mismatch: result=%#v err=%v", result, err)
	}
	changed := stateCaptureJSON(t, `{"ok":true,"data":{"name":"benchmark","target":{"build":"2"},"updatedAt":"2026-08-05T02:00:00Z"}}`)
	result, err = CompareJSONWithMode(expected, changed, ComparisonProfileStateV1)
	if err != nil || result.Equal {
		t.Fatalf("profile target change passed: result=%#v err=%v", result, err)
	}
}

func TestProfileStateComparisonNormalizesOnlyMissingProfileHome(t *testing.T) {
	expected := stateCaptureJSON(t, `{"ok":false,"error":{"message":"open C:\\capture\\profiles\\missing.json: file not found"}}`)
	actual := stateCaptureJSON(t, `{"ok":false,"error":{"message":"open D:\\run\\profiles\\missing.json: file not found"}}`)
	result, err := CompareJSONWithMode(expected, actual, ComparisonProfileStateV1)
	if err != nil || !result.Equal {
		t.Fatalf("missing profile home mismatch: result=%#v err=%v", result, err)
	}
}

func TestFixturePathIsDeterministic(t *testing.T) {
	path := FixturePath("db2", "spellname-123")
	want := "fixtures/golden/db2/spellname-123.json"
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestCompareJSONSemanticEqual(t *testing.T) {
	result, err := CompareJSON([]byte(`{"ok":true,"data":{"id":1,"name":"Spell"}}`), []byte(`{
		"data": {"name": "Spell", "id": 1},
		"ok": true
	}`))
	if err != nil {
		t.Fatalf("CompareJSON: %v", err)
	}
	if !result.Equal {
		t.Fatalf("expected semantic equality: %#v", result)
	}
}

func TestCompareJSONCapturedStdoutSemanticEqual(t *testing.T) {
	expected := []byte(`{
		"name": "expected/db2",
		"command": "wowdata-fixture",
		"args": ["db2","rows"],
		"exitCode": 0,
		"stdout": "{\"ok\":true,\"data\":{\"id\":1,\"name\":\"Spell\"}}\n",
		"stderr": ""
	}`)
	actual := []byte(`{
		"name": "go/db2",
		"command": "wowdata",
		"args": ["db2","rows"],
		"exitCode": 0,
		"stdout": "{\"data\":{\"name\":\"Spell\",\"id\":1},\"ok\":true}\n",
		"stderr": ""
	}`)

	result, err := CompareJSON(expected, actual)
	if err != nil {
		t.Fatalf("CompareJSON: %v", err)
	}
	if !result.Equal {
		t.Fatalf("expected captured stdout semantic equality: %#v", result)
	}
}

func TestCompareJSONCapturedStdoutBarePayloadMatchesGoEnvelope(t *testing.T) {
	expected := []byte(`{"exitCode":0,"stdout":"{\"input\":\"fixtures\\\\golden\\\\inputs\\\\minimal-vp9.avi\",\"frameCount\":2}\n"}`)
	actual := []byte(`{"exitCode":0,"stdout":"{\"ok\":true,\"data\":{\"input\":\"fixtures/golden/inputs/minimal-vp9.avi\",\"frameCount\":2}}\n"}`)

	result, err := CompareJSON(expected, actual)
	if err != nil {
		t.Fatalf("CompareJSON: %v", err)
	}
	if !result.Equal {
		t.Fatalf("expected captured stdout bare payload to match Go envelope: %#v", result)
	}
}

func TestCompareJSONCapturedJSONLinesMatchesStreamPayload(t *testing.T) {
	expected := []byte(`{
		"exitCode": 0,
		"stdout": "{\"ok\":true,\"command\":\"db2 stream\",\"data\":{\"table\":\"SpellEffect\",\"mode\":\"stream\",\"count\":2,\"rows\":[{\"ID\":1},{\"ID\":2}]},\"warnings\":[]}\n",
		"stderr": ""
	}`)
	actual := []byte(`{
		"exitCode": 0,
		"stdout": "{\"ok\":true,\"command\":\"db2 stream row\",\"data\":{\"table\":\"SpellEffect\",\"mode\":\"stream\",\"row\":{\"ID\":1}},\"warnings\":[]}\n{\"ok\":true,\"command\":\"db2 stream row\",\"data\":{\"table\":\"SpellEffect\",\"mode\":\"stream\",\"row\":{\"ID\":2}},\"warnings\":[]}\n",
		"stderr": ""
	}`)

	result, err := CompareJSON(expected, actual)
	if err != nil {
		t.Fatalf("CompareJSON: %v", err)
	}
	if !result.Equal {
		t.Fatalf("expected JSONL stream rows to match aggregate payload: %#v", result)
	}
}

func TestCompareJSONCapturedStdoutMismatch(t *testing.T) {
	expected := []byte(`{"exitCode":0,"stdout":"{\"ok\":true}\n"}`)
	actual := []byte(`{"exitCode":0,"stdout":"{\"ok\":false}\n"}`)

	result, err := CompareJSON(expected, actual)
	if err != nil {
		t.Fatalf("CompareJSON: %v", err)
	}
	if result.Equal {
		t.Fatal("expected captured stdout mismatch")
	}
}

func TestCompareJSONCapturedStderrIgnoresValidDownloadTelemetry(t *testing.T) {
	expected := []byte(`{"exitCode":0,"stdout":"{\"ok\":true,\"data\":{\"id\":1}}","stderr":"prepare target=remote/us/wow/enUS build=1\nprepare status=ready\n"}`)
	actual := []byte(`{"exitCode":0,"stdout":"{\"ok\":true,\"data\":{\"id\":1}}","stderr":"prepare target=remote/us/wow/enUS build=1\ndownload method=range url=https://cdn.example/object bytes=42 workers=4 duration=12ms\nprepare status=ready\n"}`)
	result, err := CompareJSON(expected, actual)
	if err != nil || !result.Equal {
		t.Fatalf("comparison = %#v, %v", result, err)
	}
}

func TestCompareJSONCapturedStderrRejectsMalformedDownloadTelemetry(t *testing.T) {
	expected := []byte(`{"exitCode":0,"stdout":"{\"ok\":true,\"data\":{\"id\":1}}","stderr":""}`)
	actual := []byte(`{"exitCode":0,"stdout":"{\"ok\":true,\"data\":{\"id\":1}}","stderr":"download method=range url=https://cdn.example/object workers=4 duration=12ms\n"}`)
	result, err := CompareJSON(expected, actual)
	if err != nil || result.Equal || !strings.Contains(result.Reason, "missing bytes") {
		t.Fatalf("comparison = %#v, %v", result, err)
	}
}

func TestCompareJSONReportsDifference(t *testing.T) {
	result, err := CompareJSON([]byte(`{"ok":true}`), []byte(`{"ok":false}`))
	if err != nil {
		t.Fatalf("CompareJSON: %v", err)
	}
	if result.Equal {
		t.Fatal("expected mismatch")
	}
	if result.Reason == "" {
		t.Fatal("expected mismatch reason")
	}
}

func TestCompareRemoteProductsAllowsBuildDriftButChecksContract(t *testing.T) {
	expected := []byte(`{"exitCode":0,"stdout":"{\"ok\":true,\"data\":{\"source\":\"remote\",\"products\":[{\"label\":\"World 1.2.3.100\",\"buildIndex\":0,\"product\":\"wow\",\"region\":\"us\",\"version\":\"1.2.3.100\",\"buildId\":\"100\",\"buildConfigKey\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"cdnConfigKey\":\"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\",\"locales\":[\"enUS\"]}]}}"}`)
	actual := []byte(`{"exitCode":0,"stdout":"{\"ok\":true,\"data\":{\"source\":\"remote\",\"products\":[{\"label\":\"World 1.2.3.101\",\"buildIndex\":0,\"product\":\"wow\",\"region\":\"us\",\"version\":\"1.2.3.101\",\"buildId\":\"101\",\"buildConfigKey\":\"cccccccccccccccccccccccccccccccc\",\"cdnConfigKey\":\"dddddddddddddddddddddddddddddddd\",\"locales\":[\"enUS\"]}]}}"}`)
	result, err := CompareJSONWithMode(expected, actual, ComparisonRemoteProductsV1)
	if err != nil || !result.Equal {
		t.Fatalf("remote products comparison = %#v, %v", result, err)
	}
}

func TestCompareRemoteProductsRejectsInconsistentIdentity(t *testing.T) {
	expected := []byte(`{"exitCode":0,"stdout":"{\"ok\":true,\"data\":{\"source\":\"remote\",\"products\":[{\"label\":\"World 1.2.3.100\",\"buildIndex\":0,\"product\":\"wow\",\"region\":\"us\",\"version\":\"1.2.3.100\",\"buildId\":\"100\",\"buildConfigKey\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"cdnConfigKey\":\"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\",\"locales\":[\"enUS\"]}]}}"}`)
	actual := []byte(`{"exitCode":0,"stdout":"{\"ok\":true,\"data\":{\"source\":\"remote\",\"products\":[{\"label\":\"World 1.2.3.101\",\"buildIndex\":0,\"product\":\"wow\",\"region\":\"us\",\"version\":\"1.2.3.101\",\"buildId\":\"999\",\"buildConfigKey\":\"cccccccccccccccccccccccccccccccc\",\"cdnConfigKey\":\"dddddddddddddddddddddddddddddddd\",\"locales\":[\"enUS\"]}]}}"}`)
	result, err := CompareJSONWithMode(expected, actual, ComparisonRemoteProductsV1)
	if err != nil || result.Equal || !strings.Contains(result.Reason, "inconsistent") {
		t.Fatalf("remote products comparison = %#v, %v", result, err)
	}
}

func TestParseManifestEntries(t *testing.T) {
	manifest, err := ParseManifest([]byte(`{
		"version": 1,
		"fixtures": [
			{
				"name":"db2/spellname",
				"fixture":"expected/db2/spellname.json",
				"actual":"go/db2/spellname.json",
				"artifacts":[{"path":"out/spell.bin","sha256":"abc","size":3}]
			}
		]
	}`))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if len(manifest.Fixtures) != 1 {
		t.Fatalf("fixtures = %d", len(manifest.Fixtures))
	}
	if manifest.Fixtures[0].Name != "db2/spellname" {
		t.Fatalf("name = %q", manifest.Fixtures[0].Name)
	}
	if len(manifest.Fixtures[0].Artifacts) != 1 {
		t.Fatalf("artifacts = %d", len(manifest.Fixtures[0].Artifacts))
	}
	if manifest.Fixtures[0].Artifacts[0].Path != "out/spell.bin" {
		t.Fatalf("artifact path = %q", manifest.Fixtures[0].Artifacts[0].Path)
	}
}

func TestManifestMissingRequiredGroups(t *testing.T) {
	manifest, err := ParseManifest([]byte(`{
		"version": 1,
		"requiredGroups": ["db2", "spell", "file"],
		"fixtures": [
			{"name":"db2/spellname","group":"db2","fixture":"expected/db2/spellname.json","actual":"go/db2/spellname.json"}
		]
	}`))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	missing := manifest.MissingRequiredGroups()
	if len(missing) != 2 || missing[0] != "spell" || missing[1] != "file" {
		t.Fatalf("missing groups = %#v", missing)
	}
}

func TestManifestMissingRequiredSuccessGroups(t *testing.T) {
	manifest, err := ParseManifest([]byte(`{
		"version": 1,
		"requiredGroups": ["db2", "file"],
		"requiredSuccessGroups": ["db2", "file"],
		"fixtures": [
			{"name":"db2/schema-without-warmup","group":"db2","kind":"error","fixture":"expected/db2/error.json","actual":"go/db2/error.json"},
			{"name":"file/search-ok","group":"file","kind":"success","fixture":"expected/file/search.json","actual":"go/file/search.json"}
		]
	}`))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	missing := manifest.MissingRequiredSuccessGroups()
	if len(missing) != 1 || missing[0] != "db2" {
		t.Fatalf("missing success groups = %#v", missing)
	}
}

func TestCompareArtifactsChecksHashAndSize(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "out.bin")
	if err := os.WriteFile(artifact, []byte("artifact bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	results := CompareArtifacts(dir, ManifestEntry{
		Name: "file/export",
		Artifacts: []ArtifactExpectation{{
			Path:   "out.bin",
			SHA256: "4659fc0570122b0e0aa14f4ff7c261b1fe51795a01ba79963f462ebf40d7520d",
			Size:   14,
		}},
	})

	if len(results) != 1 {
		t.Fatalf("results = %d", len(results))
	}
	if !results[0].Equal {
		t.Fatalf("expected artifact match: %#v", results[0])
	}
}

func TestCompareArtifactsReportsMissingFile(t *testing.T) {
	results := CompareArtifacts(t.TempDir(), ManifestEntry{
		Name: "file/export",
		Artifacts: []ArtifactExpectation{{
			Path:   "missing.bin",
			SHA256: "deadbeef",
		}},
	})

	if len(results) != 1 {
		t.Fatalf("results = %d", len(results))
	}
	if results[0].Equal {
		t.Fatal("expected missing artifact mismatch")
	}
	if results[0].Reason == "" {
		t.Fatal("expected mismatch reason")
	}
}
