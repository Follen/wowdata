package main

import (
	"encoding/json"
	"os"
	"testing"

	"wowdata/internal/app"
)

func TestBuildMatrixEnumeratesEveryCobraLeaf(t *testing.T) {
	fixtures := fixtureManifest{Fixtures: []fixture{{Name: "db2/rows", Kind: "success", Command: "wowdata", Args: []string{"db2", "rows", "SpellName", "--id", "1"}}}}
	result := buildMatrix(app.NewRootCommand(), fixtures)
	want := collectLeafPaths(app.NewRootCommand())
	if result.Summary.LeafCommands != len(want) {
		t.Fatalf("leaf commands = %d, want %d", result.Summary.LeafCommands, len(want))
	}
	seen := make(map[string]bool, len(result.Commands))
	for _, command := range result.Commands {
		if seen[command.Path] {
			t.Fatalf("duplicate command %q", command.Path)
		}
		seen[command.Path] = true
		if len(command.Cases) == 0 || len(command.RequiredProtocols) != 4 {
			t.Fatalf("command %q has incomplete matrix: %#v", command.Path, command)
		}
		if command.OutputComparison == "" {
			t.Fatalf("command %q has no output comparison policy", command.Path)
		}
		if command.BaselineComparison == "" {
			t.Fatalf("command %q has no baseline comparison policy", command.Path)
		}
		if command.GoldenComparison == "" {
			t.Fatalf("command %q has no golden comparison policy", command.Path)
		}
		for _, benchmarkCase := range command.Cases {
			if len(benchmarkCase.StableKey) != 64 {
				t.Fatalf("command %q has invalid stable case key %q", command.Path, benchmarkCase.StableKey)
			}
		}
	}
	for _, path := range want {
		if !seen[path] {
			t.Errorf("missing Cobra leaf %q", path)
		}
	}
}

func TestOnlyNewCommandAllowsUnsupportedBaseline(t *testing.T) {
	result := buildMatrix(app.NewRootCommand(), fixtureManifest{})
	for _, command := range result.Commands {
		want := "strict"
		if command.Path == "encounter export" {
			want = "unsupported-or-strict"
		}
		if command.BaselineComparison != want {
			t.Fatalf("%s baseline comparison = %q, want %q", command.Path, command.BaselineComparison, want)
		}
	}
}

func TestStatefulCommandsUseWithinProtocolOutputComparison(t *testing.T) {
	result := buildMatrix(app.NewRootCommand(), fixtureManifest{})
	want := map[string]string{
		"cache status":   "stable-within-protocol",
		"profile remove": "stable-within-protocol",
		"update":         "stable-within-protocol",
		"db2 rows":       "strict-cross-protocol",
	}
	for _, command := range result.Commands {
		if expected, ok := want[command.Path]; ok && command.OutputComparison != expected {
			t.Fatalf("%s output comparison = %q, want %q", command.Path, command.OutputComparison, expected)
		}
		if expected, ok := want[command.Path]; ok {
			goldenWant := "all-protocols"
			if expected == "stable-within-protocol" {
				goldenWant = "independent-cold-only"
			}
			if command.GoldenComparison != goldenWant {
				t.Fatalf("%s golden comparison = %q, want %q", command.Path, command.GoldenComparison, goldenWant)
			}
		}
	}
}

func TestCacheClearDeclaresExpectedRepeatWarmMutation(t *testing.T) {
	result := buildMatrix(app.NewRootCommand(), fixtureManifest{})
	for _, command := range result.Commands {
		if command.Path == "cache clear" {
			if command.RepeatWarmCache != "expected-mutation" {
				t.Fatalf("cache clear repeat-warm policy = %q", command.RepeatWarmCache)
			}
			return
		}
	}
	t.Fatal("cache clear command missing")
}

func TestStableCaseKeyTracksInputsButNotDisplayID(t *testing.T) {
	base := benchmarkCase{ID: "display-a", Command: "wowdata", Args: []string{"db2", "rows", "SpellName", "--id", "1"}, ExpectedKind: "success"}
	same := base
	same.ID = "display-b"
	changed := base
	changed.Args = []string{"db2", "rows", "SpellName", "--id", "2"}
	if withStableKey("db2 rows", base).StableKey != withStableKey("db2 rows", same).StableKey {
		t.Fatal("display-only case ID changed the stable key")
	}
	if withStableKey("db2 rows", base).StableKey == withStableKey("db2 rows", changed).StableKey {
		t.Fatal("input change did not change the stable key")
	}
}

func TestDestructiveCommandsAreRecordOnlyAndFullyIsolated(t *testing.T) {
	result := buildMatrix(app.NewRootCommand(), fixtureManifest{})
	for _, command := range result.Commands {
		if command.Path != "update" && command.Path != "uninstall" {
			continue
		}
		if command.Execution != "record-only" || command.Isolation.InstallRoot == "" {
			t.Fatalf("%s policy = %#v", command.Path, command)
		}
		if command.Isolation.Home == "" || command.Isolation.Cache == "" || command.Isolation.Output == "" {
			t.Fatalf("%s lacks complete isolation: %#v", command.Path, command.Isolation)
		}
		foundExecution := false
		for _, candidate := range command.Cases {
			if candidate.Source == "generated-isolated-install" {
				foundExecution = true
				if hasArgument(candidate.Args, "--help") || len(candidate.StableKey) != 64 {
					t.Fatalf("%s install execution case = %#v", command.Path, candidate)
				}
			}
		}
		if !foundExecution {
			t.Fatalf("%s lacks a real isolated execution case", command.Path)
		}
	}
}

func TestFixtureCommandDetectionHandlesGoRunPrefix(t *testing.T) {
	result := buildMatrix(app.NewRootCommand(), fixtureManifest{Fixtures: []fixture{{
		Name: "rows", Kind: "success", Command: "go", Args: []string{"run", ".\\cmd\\wowdata", "--region", "us", "--product", "wow_classic_era", "db2", "rows", "SpellName"},
	}}})
	for _, command := range result.Commands {
		if command.Path == "db2 rows" {
			if len(command.Cases) != 1 || command.Cases[0].Fixture != "rows" || command.Cases[0].DetectedTarget != "classic-era-us" {
				t.Fatalf("db2 rows cases = %#v", command.Cases)
			}
			if len(command.Cases[0].Args) == 0 || command.Cases[0].Args[0] != "--region" {
				t.Fatalf("go run prefix was not removed: %#v", command.Cases[0].Args)
			}
			return
		}
	}
	t.Fatal("db2 rows leaf not found")
}

func TestFixtureExecutionArgsAddsLegacyTargetDefaults(t *testing.T) {
	args := fixtureExecutionArgs(fixture{Args: []string{"--auto-warmup", "--region", "cn", "db2", "rows", "SpellName"}})
	if argumentValue(args, "--build") != "" || argumentValue(args, "--locale") != "zhCN" {
		t.Fatalf("legacy defaults missing from %#v", args)
	}
}

func TestRemoteAutoWarmupFixtureUsesOnDemandArgs(t *testing.T) {
	result := buildMatrix(app.NewRootCommand(), fixtureManifest{Fixtures: []fixture{{
		Name: "rows", Kind: "success", Command: "wowdata",
		Args: []string{"--auto-warmup", "--source", "remote", "--region", "cn", "--product", "wow", "db2", "rows", "SpellName"},
	}}})
	for _, command := range result.Commands {
		if command.Path != "db2 rows" {
			continue
		}
		candidate := command.Cases[0]
		if hasArgument(candidate.Args, "--auto-warmup") {
			t.Fatalf("current args retain full warmup: %#v", candidate.Args)
		}
		if argumentValue(candidate.Args, "--locale") != "zhCN" {
			t.Fatalf("legacy locale missing: args=%#v", candidate.Args)
		}
		return
	}
	t.Fatal("db2 rows leaf not found")
}

func TestRemoteAutoWarmupTargetInputUsesOnDemandArgs(t *testing.T) {
	original := []string{"--auto-warmup=true", "--source", "remote", "--region", "cn", "--product", "wow", "casc", "info"}
	inputs := inputCatalog{Cases: []catalogCase{{ID: "remote-info", Path: "casc info", Target: "retail-cn", Args: original}}}
	result := buildMatrixWithInputs(app.NewRootCommand(), fixtureManifest{}, inputs)
	for _, command := range result.Commands {
		if command.Path != "casc info" {
			continue
		}
		candidate := command.Cases[0]
		if hasArgument(candidate.Args, "--auto-warmup") {
			t.Fatalf("args retain deprecated warmup: %#v", candidate.Args)
		}
		return
	}
	t.Fatal("casc info leaf not found")
}

func TestCaseWithoutAutoWarmupKeepsArgsStable(t *testing.T) {
	inputs := inputCatalog{Cases: []catalogCase{{
		ID: "remote-info", Path: "casc info", Target: "retail-cn",
		Args: []string{"--source", "remote", "--region", "cn", "--product", "wow", "casc", "info"},
	}}}
	result := buildMatrixWithInputs(app.NewRootCommand(), fixtureManifest{}, inputs)
	for _, command := range result.Commands {
		if command.Path == "casc info" {
			encoded, err := json.Marshal(command.Cases[0])
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) == "" || jsonContainsKey(t, encoded, "baselineArgs") || hasArgument(command.Cases[0].Args, "--auto-warmup") {
				t.Fatalf("unexpected auto-warmup args: %s", encoded)
			}
			return
		}
	}
	t.Fatal("casc info leaf not found")
}

func jsonContainsKey(t *testing.T, data []byte, key string) bool {
	t.Helper()
	var value map[string]json.RawMessage
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	_, ok := value[key]
	return ok
}

func TestRealRemoteDataInputsUsePinnedBuilds(t *testing.T) {
	data, err := os.ReadFile("../target-inputs.json")
	if err != nil {
		t.Fatal(err)
	}
	var inputs inputCatalog
	if err := json.Unmarshal(data, &inputs); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range inputs.Cases {
		want := ""
		switch candidate.Target {
		case "retail-cn":
			want = retailCNBuild
		case "classic-era-us":
			want = classicEraUSBuild
		default:
			continue
		}
		if got := argumentValue(candidate.Args, "--build"); got != want {
			t.Errorf("case %q build = %q, want %q", candidate.ID, got, want)
		}
	}
}

func TestWildcardInputsMaterializePinnedClassicTarget(t *testing.T) {
	result := buildMatrixWithInputs(app.NewRootCommand(), fixtureManifest{}, inputCatalog{Cases: []catalogCase{{
		ID: "portable-casc-info", Path: "casc info", Target: "*", Args: []string{"casc", "info"}, ExpectedKind: "success",
	}}})
	for _, command := range result.Commands {
		if command.Path != "casc info" {
			continue
		}
		for _, candidate := range command.Cases {
			if candidate.ID != "portable-casc-info" {
				continue
			}
			if candidate.DetectedTarget != "classic-era-us" || argumentValue(candidate.Args, "--source") != "remote" || argumentValue(candidate.Args, "--region") != "us" || argumentValue(candidate.Args, "--product") != "wow_classic_era" || argumentValue(candidate.Args, "--build") != classicEraUSBuild || argumentValue(candidate.Args, "--locale") != "enUS" {
				t.Fatalf("wildcard target was not materialized: %#v", candidate)
			}
			return
		}
	}
	t.Fatal("portable casc info case not found")
}

func TestRetailCatalogInputsMaterializePinnedTargets(t *testing.T) {
	result := buildMatrixWithInputs(app.NewRootCommand(), fixtureManifest{}, inputCatalog{Cases: []catalogCase{
		{ID: "retail-remote", Path: "decor get", Target: "retail-cn", Source: "remote", Args: []string{"decor", "get", "--id", "80"}, ExpectedKind: "success"},
		{ID: "retail-local", Path: "decor get", Target: "retail-cn-local-12.0.7.68887", Source: "local", Args: []string{"decor", "get", "--id", "726"}, ExpectedKind: "success"},
	}})
	var foundRemote, foundLocal bool
	for _, command := range result.Commands {
		if command.Path != "decor get" {
			continue
		}
		for _, candidate := range command.Cases {
			switch candidate.ID {
			case "retail-remote":
				foundRemote = true
				if candidate.DetectedTarget != "retail-cn" || argumentValue(candidate.Args, "--source") != "remote" || argumentValue(candidate.Args, "--region") != "cn" || argumentValue(candidate.Args, "--product") != "wow" || argumentValue(candidate.Args, "--build") != retailCNBuild || argumentValue(candidate.Args, "--locale") != "zhCN" {
					t.Fatalf("remote retail target was not materialized: %#v", candidate)
				}
			case "retail-local":
				foundLocal = true
				if candidate.DetectedTarget != "retail-cn-local-12.0.7.68887" || argumentValue(candidate.Args, "--source") != "local" || argumentValue(candidate.Args, "--region") != "cn" || argumentValue(candidate.Args, "--product") != "wow" || argumentValue(candidate.Args, "--build") != "12.0.7.68887" || argumentValue(candidate.Args, "--locale") != "zhCN" {
					t.Fatalf("local retail target was not materialized: %#v", candidate)
				}
			}
		}
	}
	if !foundRemote || !foundLocal {
		t.Fatalf("retail catalog cases not found: remote=%v local=%v", foundRemote, foundLocal)
	}
}

func TestDataCommandsExposeDualBuildCoverageGaps(t *testing.T) {
	result := buildMatrix(app.NewRootCommand(), fixtureManifest{Fixtures: []fixture{{
		Name: "rows", Kind: "success", Command: "wowdata", Args: []string{"--region", "us", "--product", "wow_classic_era", "db2", "rows", "SpellName"},
	}}})
	for _, command := range result.Commands {
		if command.Path != "db2 rows" {
			continue
		}
		if len(command.RequiredTargets) != 2 || len(command.ObservedTargets) != 1 || command.ObservedTargets[0] != "classic-era-us" {
			t.Fatalf("target coverage = required %#v observed %#v", command.RequiredTargets, command.ObservedTargets)
		}
		if len(command.MissingTargets) != 1 || command.MissingTargets[0] != "retail-cn" {
			t.Fatalf("missing targets = %#v", command.MissingTargets)
		}
		return
	}
	t.Fatal("db2 rows leaf not found")
}

func TestAuditedNonTargetIsNotReportedAsUnresolvedGap(t *testing.T) {
	inputs := inputCatalog{NonTargets: []catalogNonTarget{{Path: "decor get", Target: "classic-era-us", Reason: "table has no rows", Evidence: "fixture.json"}}}
	result := buildMatrixWithInputs(app.NewRootCommand(), fixtureManifest{}, inputs)
	for _, command := range result.Commands {
		if command.Path == "decor get" {
			if len(command.NonTargets) != 1 || len(command.MissingTargets) != 1 || command.MissingTargets[0] != "retail-cn" {
				t.Fatalf("non-target accounting = %#v missing=%#v", command.NonTargets, command.MissingTargets)
			}
			return
		}
	}
	t.Fatal("decor get leaf not found")
}
