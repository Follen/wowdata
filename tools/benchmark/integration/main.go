// Command integration generates the exhaustive integration benchmark manifest.
// It complements, and does not replace, the small versioned golden suite.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"wowdata/internal/app"

	"github.com/spf13/cobra"
)

const integrationSchema = "wowdata.integration-benchmark-matrix.v1"

var requiredScenarios = []string{"success", "missing-args", "empty-result", "representative-error"}

type buildCatalog struct {
	Schema    string          `json:"schema"`
	Builds    []buildTarget   `json:"builds"`
	Discovery *buildDiscovery `json:"discovery,omitempty"`
}

type buildDiscovery struct {
	SourcePath string `json:"sourcePath"`
	SHA256     string `json:"sha256"`
}

type buildTarget struct {
	Name      string `json:"name"`
	Region    string `json:"region"`
	Product   string `json:"product"`
	Build     string `json:"build"`
	Locale    string `json:"locale"`
	LocalPath string `json:"localPath"`
}

type inputCatalog struct {
	Cases      []catalogCase      `json:"cases"`
	NonTargets []catalogNonTarget `json:"nonTargets"`
}

type catalogCase struct {
	ID           string     `json:"id"`
	Path         string     `json:"path"`
	Target       string     `json:"target"`           // exact Build name, product:<name>, or *
	Source       string     `json:"source,omitempty"` // local, remote, or empty for either
	Scenario     string     `json:"scenario,omitempty"`
	Args         []string   `json:"args"`
	Setup        [][]string `json:"setup,omitempty"`
	ExpectedKind string     `json:"expectedKind"`
	Evidence     string     `json:"evidence,omitempty"`
}

type catalogNonTarget struct {
	Path     string `json:"path"`
	Target   string `json:"target"`
	Source   string `json:"source,omitempty"`
	Reason   string `json:"reason"`
	Evidence string `json:"evidence"`
}

type integrationMatrix struct {
	Schema         string               `json:"schema"`
	GoldenLayer    string               `json:"goldenLayer"`
	BuildDiscovery *buildDiscovery      `json:"buildDiscovery,omitempty"`
	Protocols      []string             `json:"protocols"`
	Sources        []string             `json:"sources"`
	Builds         []buildTarget        `json:"builds"`
	Scenarios      []string             `json:"scenarios"`
	Summary        integrationSummary   `json:"summary"`
	Commands       []integrationCommand `json:"commands"`
}

type integrationSummary struct {
	LeafCommands int `json:"leafCommands"`
	Cells        int `json:"cells"`
	Executable   int `json:"executable"`
	Excluded     int `json:"excluded"`
	CoverageGaps int `json:"coverageGaps"`
}

type integrationCommand struct {
	Path  string            `json:"path"`
	Risk  string            `json:"risk"`
	Cases []integrationCase `json:"cases"`
}

type integrationCase struct {
	ID              string     `json:"id"`
	StableKey       string     `json:"stableKey"`
	WorkflowKey     string     `json:"workflowKey"`
	Build           string     `json:"build"`
	Source          string     `json:"source"`
	Protocol        string     `json:"protocol"`
	Scenario        string     `json:"scenario"`
	Args            []string   `json:"args,omitempty"`
	Setup           [][]string `json:"setup,omitempty"`
	ExpectedKind    string     `json:"expectedKind,omitempty"`
	Executable      bool       `json:"executable"`
	CoverageGap     bool       `json:"coverageGap,omitempty"`
	ExclusionReason string     `json:"exclusionReason,omitempty"`
	Evidence        string     `json:"evidence,omitempty"`
	Home            string     `json:"home"`
	Cache           string     `json:"cache"`
	Output          string     `json:"output"`
	InstallRoot     string     `json:"installRoot,omitempty"`
	Fixture         string     `json:"fixture,omitempty"`
	Capture         []string   `json:"capture"`
}

func main() {
	buildsPath := flag.String("builds", "tools/benchmark/integration/builds.json", "Build catalog")
	inputsPath := flag.String("inputs", "tools/benchmark/target-inputs.json", "integration input catalog")
	flag.Parse()
	var builds buildCatalog
	readJSON(*buildsPath, &builds)
	var inputs inputCatalog
	readJSON(*inputsPath, &inputs)
	result, err := buildIntegrationMatrix(app.NewRootCommand(), builds, inputs)
	if err != nil {
		fatal(err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fatal(err)
	}
}

func buildIntegrationMatrix(root *cobra.Command, catalog buildCatalog, inputs inputCatalog) (integrationMatrix, error) {
	if catalog.Schema != "wowdata.integration-build-catalog.v1" {
		return integrationMatrix{}, fmt.Errorf("unsupported Build catalog schema %q", catalog.Schema)
	}
	if len(catalog.Builds) == 0 {
		return integrationMatrix{}, fmt.Errorf("Build catalog is empty")
	}
	protocols := []string{"independent-cold", "shared-build-cold", "warm", "repeat-warm"}
	sources := []string{"local", "remote"}
	result := integrationMatrix{
		Schema: integrationSchema, GoldenLayer: "fixtures/golden/manifest.json", BuildDiscovery: catalog.Discovery,
		Protocols: protocols, Sources: sources, Builds: catalog.Builds,
		Scenarios: append([]string(nil), requiredScenarios...),
	}
	for _, path := range collectLeafPaths(root) {
		command := integrationCommand{Path: path, Risk: commandRisk(path)}
		for _, target := range catalog.Builds {
			for _, source := range sources {
				for _, protocol := range protocols {
					for _, scenario := range requiredScenarios {
						cell := buildCell(path, target, source, protocol, scenario, inputs)
						command.Cases = append(command.Cases, cell)
						result.Summary.Cells++
						if cell.Executable {
							result.Summary.Executable++
						} else {
							result.Summary.Excluded++
							if cell.CoverageGap {
								result.Summary.CoverageGaps++
							}
						}
					}
				}
			}
		}
		result.Commands = append(result.Commands, command)
	}
	result.Summary.LeafCommands = len(result.Commands)
	return result, nil
}

func buildCell(path string, target buildTarget, source, protocol, scenario string, inputs inputCatalog) integrationCase {
	id := slug(strings.Join([]string{path, target.Name, source, protocol, scenario}, "-"))
	workflowKey := slug(strings.Join([]string{path, target.Name, source, scenario}, "-"))
	stateRoot := "${RUN_ROOT}/shared/" + workflowKey
	if protocol == "independent-cold" {
		stateRoot = "${RUN_ROOT}/independent/" + id
	}
	cell := integrationCase{
		ID: id, WorkflowKey: workflowKey, Build: target.Name, Source: source, Protocol: protocol, Scenario: scenario,
		Home: stateRoot + "/home", Cache: stateRoot + "/home/cache",
		Output:  "${RUN_ROOT}/output/" + id,
		Capture: []string{"stdout", "stderr", "exit-code", "output-sha256", "wall-time", "cpu", "peak-heap", "peak-working-set", "gc", "requests", "connections", "network-bytes", "disk-bytes", "stage-times", "cache-delta", "fallback-reason"},
	}
	if commandRisk(path) == "install-destructive" {
		cell.InstallRoot = "${RUN_ROOT}/install/" + id
		cell.Fixture = "isolated-npm-success"
	}
	if (path == "update" || path == "uninstall") && scenario == "missing-args" {
		cell.ExclusionReason = "command accepts zero positional arguments and has no missing-arguments contract"
		cell.Evidence = "internal/app/commands.go"
		cell.StableKey = stableKey(path, cell)
		return cell
	}
	if synthetic, ok := syntheticScenario(path, target, source, scenario); ok {
		cell.Args = synthetic
		cell.ExpectedKind = "error"
		cell.Executable = true
		cell.StableKey = stableKey(path, cell)
		return cell
	}
	if scenario == "success" {
		if synthetic, setup, ok := syntheticSuccessScenario(path, target, source); ok {
			cell.Args, cell.Setup, cell.ExpectedKind, cell.Executable = synthetic, setup, "success", true
			if path == "update" || path == "uninstall" {
				cell.Evidence = "synthetic npm executable confined to installRoot"
			}
			cell.StableKey = stableKey(path, cell)
			return cell
		}
	}
	if scenario == "empty-result" {
		if args, ok := emptyResultScenario(path, target, source); ok {
			cell.Args, cell.ExpectedKind, cell.Executable = args, "empty-result", true
			cell.Evidence = "typed empty-result probe generated from the live command contract"
			cell.StableKey = stableKey(path, cell)
			return cell
		}
		cell.ExclusionReason = "command returns a singleton action or artifact and has no empty-set result contract"
		cell.Evidence = "internal/app/commands.go"
		cell.StableKey = stableKey(path, cell)
		return cell
	}
	if excluded, ok := exclusionFor(inputs.NonTargets, path, target.Name, source); ok {
		cell.ExclusionReason, cell.Evidence = excluded.Reason, excluded.Evidence
		cell.StableKey = stableKey(path, cell)
		return cell
	}
	configured, ok := inputFor(inputs.Cases, path, target, source, scenario)
	if !ok && scenario == "success" && path == "cache status" {
		configured = catalogCase{ID: "builtin-cache-status", Path: path, Target: target.Name, Args: []string{"cache", "status"}, ExpectedKind: "success"}
		ok = true
	}
	if !ok {
		cell.CoverageGap = true
		cell.ExclusionReason = "integration corpus has no " + scenario + " input for this command and Build"
		cell.StableKey = stableKey(path, cell)
		return cell
	}
	cell.Args = sourceArgs(configured.Args, source, target)
	cell.Setup = configured.Setup
	cell.Evidence = configured.Evidence
	cell.ExpectedKind = configured.ExpectedKind
	if cell.ExpectedKind == "" {
		cell.ExpectedKind = scenario
	}
	cell.Executable = true
	cell.StableKey = stableKey(path, cell)
	return cell
}

// emptyResultScenario uses values outside the observed WoW ID namespace or a
// sentinel query. These are real data-path executions, not help/error probes.
func emptyResultScenario(path string, target buildTarget, source string) ([]string, bool) {
	var args []string
	switch path {
	case "db2 rows":
		args = []string{"--dbd-manifest=true", "--tables", "SpellName", "db2", "rows", "SpellName", "--id", "4294967295"}
	case "db2 search":
		args = []string{"--dbd-manifest=true", "--tables", "SpellName", "db2", "search", "SpellName", "--field", "Name_lang", "--query", "__wowdata_integration_no_match_7f91e3__", "--limit", "3"}
	case "db2 foreign-key":
		args = []string{"--dbd-manifest=true", "--tables", "SpellEffect", "db2", "foreign-key", "SpellEffect", "--field", "SpellID", "--value", "4294967295"}
	case "spell info":
		args = []string{"--dbd-manifest=true", "--tables", "SpellEffect,Spell,SpellName,SpellMisc,SpellCastTimes,SpellDuration,SpellRange", "spell", "info", "--spell-id", "4294967295", "--max-depth", "1"}
	case "spell auras":
		args = []string{"--dbd-manifest=true", "--tables", "SpellEffect", "spell", "auras", "--spell-id", "4294967295"}
	case "spell summons":
		args = []string{"--dbd-manifest=true", "--tables", "SpellEffect", "spell", "summons", "--spell-id", "4294967295", "--npc-id", "4294967295"}
	case "encounter get":
		args = []string{"--dbd-manifest=true", "--tables", "JournalEncounterSection", "encounter", "get", "--journal-encounter-id", "4294967295"}
	case "file lookup":
		args = []string{"--listfile=true", "--listfile-format", "text", "file", "lookup", "--file-data-id", "4294967295"}
	case "file search":
		args = []string{"--listfile=true", "--listfile-format", "text", "file", "search", "--query", "__wowdata_integration_no_match_7f91e3__", "--limit", "3"}
	case "file extension":
		args = []string{"--listfile=true", "--listfile-format", "text", "file", "extension", "--extension", "wowdata_no_such_extension_7f91e3", "--limit", "3"}
	case "file exists":
		args = []string{"--listfile=false", "file", "exists", "--file-data-id", "4294967295"}
	case "item models":
		args = []string{"--dbd-manifest=true", "--tables", "Item,ItemSparse,ItemModifiedAppearance,ItemAppearance,ItemDisplayInfo,ModelFileData,TextureFileData,ComponentModelFileData", "item", "models", "--item-id", "4294967295", "--race-id", "1", "--gender", "0"}
	case "item geosets":
		args = []string{"--dbd-manifest=true", "--tables", "Item,ItemSparse,ItemModifiedAppearance,ItemAppearance,ItemDisplayInfo,HelmetGeosetData", "item", "geosets", "--item-id", "4294967295"}
	case "item textures":
		args = []string{"--dbd-manifest=true", "--tables", "Item,ItemSparse,ItemModifiedAppearance,ItemAppearance,ItemDisplayInfo,ItemDisplayInfoMaterialRes,TextureFileData", "item", "textures", "--item-id", "4294967295"}
	case "creature model":
		args = []string{"--dbd-manifest=true", "--tables", "CreatureDisplayInfo,CreatureModelData,CreatureDisplayInfoGeosetData", "creature", "model", "--file-data-id", "4294967295"}
	default:
		return nil, false
	}
	return sourceArgs(args, source, target), true
}

func syntheticScenario(path string, _ buildTarget, _ string, scenario string) ([]string, bool) {
	if scenario != "missing-args" && scenario != "representative-error" {
		return nil, false
	}
	args := strings.Fields(path)
	if scenario == "representative-error" {
		args = append(args, "--wowdata-integration-invalid-flag")
	}
	return args, true
}

func syntheticSuccessScenario(path string, target buildTarget, source string) ([]string, [][]string, bool) {
	base := strings.Fields(path)
	switch path {
	case "warmup":
		return sourceArgs(append(base, "--listfile=false", "--dbd-manifest=false", "--tables="), source, target), nil, true
	case "casc products":
		return sourceArgs(base, source, target), nil, true
	case "video demux":
		return []string{"video", "demux", "--input", "fixtures/golden/inputs/minimal-vp9.avi", "--output", "video-frames"}, nil, true
	case "golden compare":
		return []string{"golden", "compare", "--fixture", "fixtures/golden/go/cache/status-empty.json", "--actual", "fixtures/golden/go/cache/status-empty.json"}, nil, true
	case "golden capture":
		return []string{"golden", "capture", "--name", "integration/cache-status", "--command", "wowdata cache status", "--output", "cache-status-capture.json"}, nil, true
	case "doctor", "profile list", "cache status", "cache verify", "cache prune", "cache clear", "cache config":
		return sourceArgs(base, source, target), nil, true
	case "profile set":
		return sourceArgs(append(base, "integration-profile"), source, target), nil, true
	case "profile show", "profile remove":
		profileSet := sourceArgs([]string{"profile", "set", "integration-profile"}, source, target)
		return sourceArgs(append(base, "integration-profile"), source, target), [][]string{profileSet}, true
	case "update":
		return []string{"update", "--version", "1.2.3"}, nil, true
	case "uninstall":
		return []string{"uninstall", "--keep-data"}, nil, true
	default:
		return nil, nil, false
	}
}

func inputFor(inputs []catalogCase, path string, target buildTarget, source, scenario string) (catalogCase, bool) {
	// Prefer Build-specific evidence over product-family and globally portable
	// corpus entries, then prefer evidence measured on the requested source.
	selectors := []string{target.Name, "product:" + target.Product, "*"}
	for _, selector := range selectors {
		for _, candidateSource := range []string{source, ""} {
			for _, candidate := range inputs {
				candidateScenario := candidate.Scenario
				if candidateScenario == "" {
					candidateScenario = "success"
				}
				if candidate.Path == path && candidate.Target == selector && candidate.Source == candidateSource && candidateScenario == scenario {
					return candidate, true
				}
			}
		}
	}
	return catalogCase{}, false
}

func exclusionFor(values []catalogNonTarget, path, target, source string) (catalogNonTarget, bool) {
	for _, candidateSource := range []string{source, ""} {
		for _, value := range values {
			if value.Path == path && value.Target == target && value.Source == candidateSource && value.Reason != "" && value.Evidence != "" {
				return value, true
			}
		}
	}
	return catalogNonTarget{}, false
}

func sourceArgs(args []string, source string, target buildTarget) []string {
	result := append([]string(nil), args...)
	result = setFlag(result, "--source", source)
	result = setFlag(result, "--region", target.Region)
	result = setFlag(result, "--product", target.Product)
	result = setFlag(result, "--build", target.Build)
	result = setFlag(result, "--locale", target.Locale)
	result = removeFlag(result, "--auto-warmup")
	if source == "local" {
		result = setFlag(result, "--path", target.LocalPath)
	} else {
		result = removeFlag(result, "--path")
	}
	return result
}

func setFlag(args []string, name, value string) []string {
	args = removeFlag(args, name)
	return append([]string{name, value}, args...)
}

func removeFlag(args []string, name string) []string {
	result := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == name {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				i++
			}
			continue
		}
		if strings.HasPrefix(args[i], name+"=") {
			continue
		}
		result = append(result, args[i])
	}
	return result
}

func stableKey(path string, cell integrationCase) string {
	value := struct {
		Path, Build, Source, Protocol, Scenario string
		Args                                    []string
	}{path, cell.Build, cell.Source, cell.Protocol, cell.Scenario, cell.Args}
	data, _ := json.Marshal(value)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func collectLeafPaths(root *cobra.Command) []string {
	var result []string
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		children := command.Commands()
		if len(children) == 0 {
			result = append(result, strings.TrimPrefix(command.CommandPath(), root.Name()+" "))
			return
		}
		for _, child := range children {
			walk(child)
		}
	}
	walk(root)
	sort.Strings(result)
	return result
}

func commandRisk(path string) string {
	switch path {
	case "update", "uninstall":
		return "install-destructive"
	case "cache clear", "cache prune":
		return "cache-destructive"
	case "cache config", "profile set", "profile remove", "golden capture":
		return "state-mutating"
	case "encounter export", "file get", "file export", "icon export", "video demux":
		return "output-writing"
	default:
		return "read-only"
	}
}

func slug(value string) string {
	return strings.Trim(strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, strings.ToLower(value)), "-")
}

func readJSON(path string, target any) {
	data, err := os.ReadFile(path)
	if err != nil {
		fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		fatal(fmt.Errorf("parse %s: %w", path, err))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
