package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"wowdata/internal/app"
)

func TestMatrixEnumeratesEveryLeafBuildSourceProtocolAndScenario(t *testing.T) {
	catalog := buildCatalog{Schema: "wowdata.integration-build-catalog.v1", Builds: []buildTarget{
		{Name: "retail", Region: "cn", Product: "wow", Build: "1", Locale: "zhCN", LocalPath: "${WOWDATA_GAME_DIR}"},
		{Name: "classic", Region: "us", Product: "wow_classic_era", Build: "2", Locale: "enUS", LocalPath: "${WOWDATA_GAME_DIR}"},
	}}
	result, err := buildIntegrationMatrix(app.NewRootCommand(), catalog, inputCatalog{})
	if err != nil {
		t.Fatal(err)
	}
	leaves := collectLeafPaths(app.NewRootCommand())
	wantCells := len(leaves) * len(catalog.Builds) * len(result.Sources) * len(result.Protocols) * len(requiredScenarios)
	if result.Summary.LeafCommands != len(leaves) || result.Summary.Cells != wantCells {
		t.Fatalf("summary = %#v, want leaves=%d cells=%d", result.Summary, len(leaves), wantCells)
	}
	seen := map[string]bool{}
	for _, command := range result.Commands {
		seen[command.Path] = true
	}
	for _, leaf := range leaves {
		if !seen[leaf] {
			t.Fatalf("missing Cobra leaf %q", leaf)
		}
	}
}

func TestConfiguredSuccessExpandsToLocalAndRemoteWithoutAutoWarmup(t *testing.T) {
	target := buildTarget{Name: "retail", Region: "cn", Product: "wow", Build: "1", Locale: "zhCN", LocalPath: "${WOWDATA_GAME_DIR}"}
	inputs := inputCatalog{Cases: []catalogCase{{
		ID: "rows", Path: "db2 rows", Target: "retail", Args: []string{"--auto-warmup", "--source", "remote", "db2", "rows", "SpellName"}, ExpectedKind: "success",
	}}}
	local := buildCell("db2 rows", target, "local", "independent-cold", "success", inputs)
	remote := buildCell("db2 rows", target, "remote", "independent-cold", "success", inputs)
	if !local.Executable || !remote.Executable {
		t.Fatalf("configured cells must execute: local=%#v remote=%#v", local, remote)
	}
	if strings.Contains(strings.Join(local.Args, " "), "auto-warmup") || strings.Contains(strings.Join(remote.Args, " "), "auto-warmup") {
		t.Fatal("integration args retained forbidden full warmup")
	}
	if !reflect.DeepEqual(local.Args[:4], []string{"--path", "${WOWDATA_GAME_DIR}", "--locale", "zhCN"}) {
		t.Fatalf("local args do not start with local target flags: %#v", local.Args)
	}
	if strings.Contains(strings.Join(remote.Args, " "), "--path") {
		t.Fatalf("remote args contain local path: %#v", remote.Args)
	}
}

func TestProtocolsShareStateOnlyWithinWorkflow(t *testing.T) {
	target := buildTarget{Name: "retail", Region: "cn", Product: "wow", Build: "1", Locale: "zhCN"}
	shared := buildCell("db2 rows", target, "local", "shared-build-cold", "success", inputCatalog{})
	warm := buildCell("db2 rows", target, "local", "warm", "success", inputCatalog{})
	repeat := buildCell("db2 rows", target, "local", "repeat-warm", "success", inputCatalog{})
	cold := buildCell("db2 rows", target, "local", "independent-cold", "success", inputCatalog{})
	if shared.WorkflowKey == "" || shared.WorkflowKey != warm.WorkflowKey || shared.WorkflowKey != repeat.WorkflowKey {
		t.Fatalf("shared workflow keys = %q %q %q", shared.WorkflowKey, warm.WorkflowKey, repeat.WorkflowKey)
	}
	if shared.Home != warm.Home || warm.Home != repeat.Home || cold.Home == shared.Home {
		t.Fatalf("protocol state paths = shared=%q warm=%q repeat=%q cold=%q", shared.Home, warm.Home, repeat.Home, cold.Home)
	}
}

func TestUpdateSuccessUsesIsolatedNPMFixture(t *testing.T) {
	target := buildTarget{Name: "retail", Region: "cn", Product: "wow", Build: "1", Locale: "zhCN"}
	cell := buildCell("update", target, "remote", "warm", "success", inputCatalog{})
	if !cell.Executable || cell.CoverageGap || cell.ExpectedKind != "success" {
		t.Fatalf("update success fixture is not executable: %#v", cell)
	}
	if cell.Fixture != "isolated-npm-success" || !strings.Contains(cell.InstallRoot, "${RUN_ROOT}/install/") {
		t.Fatalf("update fixture is not install-root isolated: %#v", cell)
	}
}

func TestInstallManagementScenariosNeverInvokeUnisolatedNPM(t *testing.T) {
	target := buildTarget{Name: "retail", Region: "cn", Product: "wow", Build: "1", Locale: "zhCN"}
	for _, path := range []string{"update", "uninstall"} {
		success := buildCell(path, target, "local", "independent-cold", "success", inputCatalog{})
		if !success.Executable || success.Fixture != "isolated-npm-success" || success.InstallRoot == "" {
			t.Fatalf("%s success fixture = %#v", path, success)
		}
		missing := buildCell(path, target, "local", "independent-cold", "missing-args", inputCatalog{})
		if missing.Executable || missing.CoverageGap || missing.ExclusionReason == "" {
			t.Fatalf("%s missing-args contract = %#v", path, missing)
		}
		failure := buildCell(path, target, "local", "independent-cold", "representative-error", inputCatalog{})
		if !failure.Executable || failure.Fixture != "isolated-npm-success" || failure.InstallRoot == "" {
			t.Fatalf("%s error fixture is not isolated: %#v", path, failure)
		}
	}
}

func TestEmptyResultWithoutCollectionContractIsExplicitNonTarget(t *testing.T) {
	target := buildTarget{Name: "retail", Region: "cn", Product: "wow", Build: "1", Locale: "zhCN"}
	cell := buildCell("doctor", target, "remote", "warm", "empty-result", inputCatalog{})
	if cell.Executable || cell.CoverageGap || cell.ExclusionReason == "" || cell.Evidence != "internal/app/commands.go" {
		t.Fatalf("non-collection empty scenario = %#v", cell)
	}
}

func TestCacheStatusHasBuiltInExecutableSuccessCell(t *testing.T) {
	target := buildTarget{Name: "retail", Region: "cn", Product: "wow", Build: "1", Locale: "zhCN", LocalPath: "${WOWDATA_GAME_DIR}"}
	cell := buildCell("cache status", target, "local", "independent-cold", "success", inputCatalog{})
	if !cell.Executable || cell.CoverageGap || !strings.Contains(strings.Join(cell.Args, " "), "cache status") {
		t.Fatalf("cache status built-in input invalid: %#v", cell)
	}
}

func TestNegativeScenariosAreExecutableWithoutCorpus(t *testing.T) {
	target := buildTarget{Name: "retail", Region: "cn", Product: "wow", Build: "1", Locale: "zhCN"}
	for _, scenario := range []string{"missing-args", "representative-error"} {
		cell := buildCell("db2 rows", target, "remote", "independent-cold", scenario, inputCatalog{})
		if !cell.Executable || cell.CoverageGap || cell.ExpectedKind != "error" || len(cell.Args) == 0 {
			t.Fatalf("%s probe = %#v", scenario, cell)
		}
	}
}

func TestManagementSuccessCasesUseIsolatedSetup(t *testing.T) {
	target := buildTarget{Name: "retail", Region: "cn", Product: "wow", Build: "1", Locale: "zhCN"}
	for _, path := range []string{"cache status", "doctor", "profile list", "profile show", "profile remove", "profile set"} {
		cell := buildCell(path, target, "local", "independent-cold", "success", inputCatalog{})
		if !cell.Executable || cell.ExpectedKind != "success" {
			t.Fatalf("%s case = %#v", path, cell)
		}
		if (path == "profile show" || path == "profile remove") && len(cell.Setup) != 1 {
			t.Fatalf("%s missing profile setup: %#v", path, cell)
		}
	}
}

func TestKnownBuildExclusionIsNotCountedAsCoverageGap(t *testing.T) {
	target := buildTarget{Name: "classic", Region: "us", Product: "wow_classic_era", Build: "2", Locale: "enUS"}
	inputs := inputCatalog{NonTargets: []catalogNonTarget{{Path: "decor get", Target: "classic", Reason: "table has no rows", Evidence: "fixture.json"}}}
	cell := buildCell("decor get", target, "local", "repeat-warm", "success", inputs)
	if cell.Executable || cell.CoverageGap || cell.Evidence != "fixture.json" {
		t.Fatalf("explicit exclusion classified incorrectly: %#v", cell)
	}
}

func TestCatalogSelectionPrefersExactThenProductThenPortable(t *testing.T) {
	target := buildTarget{Name: "retail-cn", Product: "wow"}
	inputs := []catalogCase{
		{ID: "portable", Path: "db2 rows", Target: "*", Args: []string{"portable"}},
		{ID: "product", Path: "db2 rows", Target: "product:wow", Args: []string{"product"}},
		{ID: "exact", Path: "db2 rows", Target: "retail-cn", Args: []string{"exact"}},
	}
	got, ok := inputFor(inputs, "db2 rows", target, "remote", "success")
	if !ok || got.ID != "exact" {
		t.Fatalf("selection = %#v, %v", got, ok)
	}
	got, ok = inputFor(inputs[:2], "db2 rows", target, "remote", "success")
	if !ok || got.ID != "product" {
		t.Fatalf("product selection = %#v, %v", got, ok)
	}
}

func TestCatalogSelectionPrefersRequestedSource(t *testing.T) {
	target := buildTarget{Name: "retail-cn", Product: "wow"}
	inputs := []catalogCase{
		{ID: "generic", Path: "decor get", Target: "retail-cn", Args: []string{"generic"}},
		{ID: "local", Path: "decor get", Target: "retail-cn", Source: "local", Args: []string{"local"}},
		{ID: "remote", Path: "decor get", Target: "retail-cn", Source: "remote", Args: []string{"remote"}},
	}
	for _, source := range []string{"local", "remote"} {
		got, ok := inputFor(inputs, "decor get", target, source, "success")
		if !ok || got.ID != source {
			t.Fatalf("%s selection = %#v, %v", source, got, ok)
		}
	}
}

func TestNonTargetDoesNotSuppressNegativeScenarios(t *testing.T) {
	target := buildTarget{Name: "classic", Region: "cn", Product: "wow_classic", Build: "1", Locale: "zhCN"}
	inputs := inputCatalog{NonTargets: []catalogNonTarget{{Path: "encounter export", Target: "classic", Source: "local", Reason: "no spell rows", Evidence: "probe.json"}}}
	success := buildCell("encounter export", target, "local", "independent-cold", "success", inputs)
	if success.Executable || success.CoverageGap || success.Evidence != "probe.json" {
		t.Fatalf("success exclusion = %#v", success)
	}
	negative := buildCell("encounter export", target, "local", "independent-cold", "representative-error", inputs)
	if !negative.Executable || negative.ExpectedKind != "error" {
		t.Fatalf("negative probe suppressed by data exclusion: %#v", negative)
	}
}

func TestTypedEmptyResultCasesAreExecutableSuccessPaths(t *testing.T) {
	target := buildTarget{Name: "retail", Region: "cn", Product: "wow", Build: "1", Locale: "zhCN", LocalPath: "${WOWDATA_GAME_DIR}"}
	for _, path := range []string{"db2 rows", "db2 search", "spell info", "file search", "file exists", "item models", "creature model"} {
		cell := buildCell(path, target, "local", "independent-cold", "empty-result", inputCatalog{})
		if !cell.Executable || cell.CoverageGap || cell.ExpectedKind != "empty-result" || !strings.Contains(strings.Join(cell.Args, " "), "--path ${WOWDATA_GAME_DIR}") {
			t.Fatalf("%s empty case = %#v", path, cell)
		}
	}
}

func TestRejectsEmptyOrUnknownBuildCatalog(t *testing.T) {
	if _, err := buildIntegrationMatrix(app.NewRootCommand(), buildCatalog{}, inputCatalog{}); err == nil {
		t.Fatal("unknown catalog schema accepted")
	}
	if _, err := buildIntegrationMatrix(app.NewRootCommand(), buildCatalog{Schema: "wowdata.integration-build-catalog.v1"}, inputCatalog{}); err == nil {
		t.Fatal("empty Build catalog accepted")
	}
}

func TestCheckedInDomainCorpusHasNoBuildSourceGaps(t *testing.T) {
	var catalog buildCatalog
	readTestJSON(t, "builds.json", &catalog)
	var inputs inputCatalog
	readTestJSON(t, "../target-inputs.json", &inputs)
	matrix, err := buildIntegrationMatrix(app.NewRootCommand(), catalog, inputs)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range matrix.Commands {
		if command.Path != "decor get" && command.Path != "encounter export" {
			continue
		}
		for _, cell := range command.Cases {
			if cell.CoverageGap {
				t.Errorf("%s Build=%s source=%s protocol=%s scenario=%s remains a coverage gap", command.Path, cell.Build, cell.Source, cell.Protocol, cell.Scenario)
			}
		}
	}
}

func readTestJSON(t *testing.T, path string, target interface{}) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}
