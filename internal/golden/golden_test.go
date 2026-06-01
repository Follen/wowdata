package golden

import (
	"os"
	"path/filepath"
	"testing"
)

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
		"result": {"content": [{"type":"text","text":"{\"table\":\"SpellEffect\",\"mode\":\"stream\",\"count\":2,\"rows\":[{\"ID\":1},{\"ID\":2}]}"}]}
	}`)
	actual := []byte(`{
		"exitCode": 0,
		"stdout": "{\"ok\":true,\"command\":\"query stream row\",\"data\":{\"table\":\"SpellEffect\",\"mode\":\"stream\",\"row\":{\"ID\":1}},\"warnings\":[]}\n{\"ok\":true,\"command\":\"query stream row\",\"data\":{\"table\":\"SpellEffect\",\"mode\":\"stream\",\"row\":{\"ID\":2}},\"warnings\":[]}\n",
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

func TestCompareJSONMCPTextResultMatchesGoEnvelopeData(t *testing.T) {
	expected := []byte(`{
		"content": [{
			"type": "text",
			"text": "{\"fileDataID\":456,\"fileName\":\"interface/icons/spell.blp\"}"
		}]
	}`)
	actual := []byte(`{
		"ok": true,
		"command": "file lookup",
		"data": {"fileName":"interface/icons/spell.blp","fileDataID":456},
		"warnings": []
	}`)

	result, err := CompareJSON(expected, actual)
	if err != nil {
		t.Fatalf("CompareJSON: %v", err)
	}
	if !result.Equal {
		t.Fatalf("expected MCP text payload to match Go data envelope: %#v", result)
	}
}

func TestCompareJSONCapturedMCPResultMatchesCapturedGoStdout(t *testing.T) {
	expected := []byte(`{
		"name": "errors/file-lookup-without-warmup",
		"tool": "wow_file",
		"args": {"action":"lookup","fileDataID":456},
		"result": {
			"isError": true,
			"content": [{"type":"text","text":"Error: file runtime is not initialized"}]
		}
	}`)
	actual := []byte(`{
		"name": "errors/file-lookup-without-warmup",
		"command": "wowdata",
		"args": ["file","lookup","--file-data-id","456"],
		"exitCode": 0,
		"stdout": "{\"ok\":false,\"error\":{\"code\":\"not_ready\",\"message\":\"file runtime is not initialized\"}}\n",
		"stderr": ""
	}`)

	result, err := CompareJSON(expected, actual)
	if err != nil {
		t.Fatalf("CompareJSON: %v", err)
	}
	if !result.Equal {
		t.Fatalf("expected captured MCP result to match captured Go stdout: %#v", result)
	}
}

func TestCompareJSONMCPErrorResultMatchesGoErrorEnvelope(t *testing.T) {
	expected := []byte(`{
		"isError": true,
		"content": [{"type":"text","text":"Error: CASC 未就绪，请先调用 wow_warmup"}]
	}`)
	actual := []byte(`{
		"ok": false,
		"command": "file lookup",
		"data": null,
		"warnings": [],
		"error": {"code":"not_ready","message":"CASC 未就绪，请先调用 wow_warmup"}
	}`)

	result, err := CompareJSON(expected, actual)
	if err != nil {
		t.Fatalf("CompareJSON: %v", err)
	}
	if !result.Equal {
		t.Fatalf("expected MCP error payload to match Go error envelope: %#v", result)
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
