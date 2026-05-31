package app

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenCompareFixtureAndActual(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture.json")
	actual := filepath.Join(dir, "actual.json")
	if err := os.WriteFile(fixture, []byte(`{"ok":true,"data":{"id":1}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(actual, []byte(`{"data":{"id":1},"ok":true}`), 0644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := executeCommandWithService(t, &Service{Golden: NewGoldenHandler()}, "golden", "compare", "--fixture", fixture, "--actual", actual)
	if err != nil {
		t.Fatalf("golden compare returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) || !strings.Contains(stdout, `"equal": true`) {
		t.Fatalf("expected equal compare success:\n%s", stdout)
	}
}

func TestGoldenCompareFixtureAndActualCapturedStdout(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture.json")
	actual := filepath.Join(dir, "actual.json")
	os.WriteFile(fixture, []byte(`{"name":"expected","command":"wowdata-fixture","exitCode":0,"stdout":"{\"ok\":true,\"data\":{\"id\":1}}\n","stderr":""}`), 0644)
	os.WriteFile(actual, []byte(`{"name":"go","command":"wowdata","exitCode":0,"stdout":"{\"data\":{\"id\":1},\"ok\":true}\n","stderr":""}`), 0644)

	stdout, stderr, err := executeCommandWithService(t, &Service{Golden: NewGoldenHandler()}, "golden", "compare", "--fixture", fixture, "--actual", actual)
	if err != nil {
		t.Fatalf("golden compare returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) || !strings.Contains(stdout, `"equal": true`) {
		t.Fatalf("expected captured stdout compare success:\n%s", stdout)
	}
}

func TestGoldenCompareReportsMismatch(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture.json")
	actual := filepath.Join(dir, "actual.json")
	os.WriteFile(fixture, []byte(`{"ok":true}`), 0644)
	os.WriteFile(actual, []byte(`{"ok":false}`), 0644)

	stdout, stderr, err := executeCommandWithService(t, &Service{Golden: NewGoldenHandler()}, "golden", "compare", "--fixture", fixture, "--actual", actual)
	if err != nil {
		t.Fatalf("golden compare returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": false`) || !strings.Contains(stdout, `"compare_failed"`) {
		t.Fatalf("expected compare failure:\n%s", stdout)
	}
}

func TestGoldenCompareAllUsesManifestPairs(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture.json")
	actual := filepath.Join(dir, "actual.json")
	manifest := filepath.Join(dir, "manifest.json")
	os.WriteFile(fixture, []byte(`{"ok":true}`), 0644)
	os.WriteFile(actual, []byte(`{"ok":true}`), 0644)
	os.WriteFile(manifest, []byte(`{"version":1,"fixtures":[{"name":"one","fixture":"`+filepath.ToSlash(fixture)+`","actual":"`+filepath.ToSlash(actual)+`"}]}`), 0644)

	stdout, stderr, err := executeCommandWithService(t, &Service{Golden: NewGoldenHandler()}, "golden", "compare", "--all", "--fixture", manifest)
	if err != nil {
		t.Fatalf("golden compare --all returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) || !strings.Contains(stdout, `"failed": 0`) {
		t.Fatalf("expected compare-all success:\n%s", stdout)
	}
}

func TestGoldenCompareAllFailsWhenRequiredGroupsMissing(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture.json")
	actual := filepath.Join(dir, "actual.json")
	manifest := filepath.Join(dir, "manifest.json")
	os.WriteFile(fixture, []byte(`{"ok":true}`), 0644)
	os.WriteFile(actual, []byte(`{"ok":true}`), 0644)
	os.WriteFile(manifest, []byte(`{
		"version":1,
		"requiredGroups":["db2","spell"],
		"fixtures":[{"name":"db2/one","group":"db2","fixture":"`+filepath.ToSlash(fixture)+`","actual":"`+filepath.ToSlash(actual)+`"}]
	}`), 0644)

	stdout, stderr, err := executeCommandWithService(t, &Service{Golden: NewGoldenHandler()}, "golden", "compare", "--all", "--fixture", manifest)
	if err != nil {
		t.Fatalf("golden compare --all returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": false`) || !strings.Contains(stdout, `"missing_required_groups"`) || !strings.Contains(stdout, "spell") {
		t.Fatalf("expected missing required groups failure:\n%s", stdout)
	}
}

func TestGoldenCompareAllFailsForEmptyRequiredManifest(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "manifest.json")
	os.WriteFile(manifest, []byte(`{"version":1,"requiredGroups":["db2"],"fixtures":[]}`), 0644)

	stdout, stderr, err := executeCommandWithService(t, &Service{Golden: NewGoldenHandler()}, "golden", "compare", "--all", "--fixture", manifest)
	if err != nil {
		t.Fatalf("golden compare --all returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": false`) || !strings.Contains(stdout, `"missing_required_groups"`) {
		t.Fatalf("expected empty manifest failure:\n%s", stdout)
	}
}

func TestGoldenCompareAllChecksArtifactHashes(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture.json")
	actual := filepath.Join(dir, "actual.json")
	artifact := filepath.Join(dir, "out.bin")
	manifest := filepath.Join(dir, "manifest.json")
	os.WriteFile(fixture, []byte(`{"ok":true}`), 0644)
	os.WriteFile(actual, []byte(`{"ok":true}`), 0644)
	artifactData := []byte("artifact bytes")
	os.WriteFile(artifact, artifactData, 0644)
	sum := sha256.Sum256(artifactData)
	os.WriteFile(manifest, []byte(`{
		"version":1,
		"fixtures":[{
			"name":"file/export",
			"fixture":"`+filepath.ToSlash(fixture)+`",
			"actual":"`+filepath.ToSlash(actual)+`",
			"artifacts":[{"path":"out.bin","sha256":"`+fmt.Sprintf("%x", sum)+`","size":14}]
		}]
	}`), 0644)

	stdout, stderr, err := executeCommandWithService(t, &Service{Golden: NewGoldenHandler()}, "golden", "compare", "--all", "--fixture", manifest)
	if err != nil {
		t.Fatalf("golden compare --all returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) || !strings.Contains(stdout, `"failed": 0`) {
		t.Fatalf("expected compare-all artifact success:\n%s", stdout)
	}
}

func TestGoldenCompareAllFailsWhenArtifactHashDiffers(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture.json")
	actual := filepath.Join(dir, "actual.json")
	artifact := filepath.Join(dir, "out.bin")
	manifest := filepath.Join(dir, "manifest.json")
	os.WriteFile(fixture, []byte(`{"ok":true}`), 0644)
	os.WriteFile(actual, []byte(`{"ok":true}`), 0644)
	os.WriteFile(artifact, []byte("artifact bytes"), 0644)
	os.WriteFile(manifest, []byte(`{
		"version":1,
		"fixtures":[{
			"name":"file/export",
			"fixture":"`+filepath.ToSlash(fixture)+`",
			"actual":"`+filepath.ToSlash(actual)+`",
			"artifacts":[{"path":"out.bin","sha256":"deadbeef","size":14}]
		}]
	}`), 0644)

	stdout, stderr, err := executeCommandWithService(t, &Service{Golden: NewGoldenHandler()}, "golden", "compare", "--all", "--fixture", manifest)
	if err != nil {
		t.Fatalf("golden compare --all returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": false`) || !strings.Contains(stdout, `"compare_failed"`) || !strings.Contains(stdout, "sha256") {
		t.Fatalf("expected artifact hash failure:\n%s", stdout)
	}
}

func TestGoldenCaptureRunsCommandAndWritesFixture(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "capture.json")
	stdout, stderr, err := executeCommandWithService(t, &Service{Golden: NewGoldenHandler()}, "golden", "capture", "--name", "go/version", "--command", "go version", "--output", outPath)
	if err != nil {
		t.Fatalf("golden capture returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("expected capture success:\n%s", stdout)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected captured fixture: %v", err)
	}
	if !strings.Contains(string(data), "go version") {
		t.Fatalf("capture should contain command output:\n%s", data)
	}
}

func TestGoldenCapturePreservesQuotedCommandArguments(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "capture.json")
	helperPath := filepath.Join(dir, "print_args.go")
	helperSource := `package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	data, _ := json.Marshal(os.Args[1:])
	fmt.Println(string(data))
}
`
	if err := os.WriteFile(helperPath, []byte(helperSource), 0644); err != nil {
		t.Fatal(err)
	}

	command := "go run " + shellQuote(helperPath) + " --path " + shellQuote(`D:\Game\World of Warcraft`)
	stdout, stderr, err := executeCommandWithService(t, &Service{Golden: NewGoldenHandler()}, "golden", "capture", "--name", "quoted/path", "--command", command, "--output", outPath)
	if err != nil {
		t.Fatalf("golden capture returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("expected capture success:\n%s", stdout)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected captured fixture: %v", err)
	}
	if !strings.Contains(string(data), `D:\\Game\\World of Warcraft`) {
		t.Fatalf("quoted path was not preserved:\n%s", data)
	}
}

func TestGoldenCaptureUpdatesManifest(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "capture.json")
	manifestPath := filepath.Join(dir, "manifest.json")

	stdout, stderr, err := executeCommandWithService(t, &Service{Golden: NewGoldenHandler()}, "golden", "capture", "--name", "go/version", "--command", "go version", "--output", outPath, "--manifest", manifestPath)
	if err != nil {
		t.Fatalf("golden capture returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("expected capture success:\n%s", stdout)
	}
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("expected manifest: %v", err)
	}
	if !strings.Contains(string(manifestData), `"name": "go/version"`) {
		t.Fatalf("manifest missing entry:\n%s", manifestData)
	}
}

func shellQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
