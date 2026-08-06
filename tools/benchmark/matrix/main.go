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

const schema = "wowdata.command-benchmark-matrix.v1"

const (
	retailCNBuild     = "12.0.7.68974"
	classicEraUSBuild = "1.15.9.69109"
)

type fixtureManifest struct {
	Fixtures []fixture `json:"fixtures"`
}

type inputCatalog struct {
	Cases      []catalogCase      `json:"cases"`
	NonTargets []catalogNonTarget `json:"nonTargets"`
}

type catalogNonTarget struct {
	Path     string `json:"path"`
	Target   string `json:"target"`
	Reason   string `json:"reason"`
	Evidence string `json:"evidence"`
}

type catalogCase struct {
	ID           string     `json:"id"`
	Path         string     `json:"path"`
	Target       string     `json:"target"`
	Source       string     `json:"source"`
	Args         []string   `json:"args"`
	Setup        [][]string `json:"setup,omitempty"`
	ExpectedKind string     `json:"expectedKind"`
}

type fixture struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type matrix struct {
	Schema    string             `json:"schema"`
	Protocols []protocol         `json:"protocols"`
	Targets   []target           `json:"targets"`
	Summary   summary            `json:"summary"`
	Commands  []commandBenchmark `json:"commands"`
}

type protocol struct {
	Name       string `json:"name"`
	FreshHome  bool   `json:"freshHome"`
	SharedHome bool   `json:"sharedHome"`
	CacheDelta string `json:"cacheDelta"`
}

type target struct {
	Name    string `json:"name"`
	Source  string `json:"source"`
	Region  string `json:"region"`
	Product string `json:"product"`
	Build   string `json:"build"`
	Locale  string `json:"locale"`
}

type summary struct {
	LeafCommands        int `json:"leafCommands"`
	Behavioral          int `json:"behavioral"`
	GeneratedSmoke      int `json:"generatedSmoke"`
	RecordOnly          int `json:"recordOnly"`
	FixtureCaseCount    int `json:"fixtureCaseCount"`
	ProtocolCaseCount   int `json:"protocolCaseCount"`
	TargetCoverageGaps  int `json:"targetCoverageGaps"`
	NonTargetExclusions int `json:"nonTargetExclusions"`
}

type commandBenchmark struct {
	Path               string             `json:"path"`
	Risk               string             `json:"risk"`
	TargetMode         string             `json:"targetMode"`
	OutputComparison   string             `json:"outputComparison"`
	RepeatWarmCache    string             `json:"repeatWarmCache"`
	BaselineComparison string             `json:"baselineComparison"`
	GoldenComparison   string             `json:"goldenComparison"`
	Execution          string             `json:"execution"`
	Isolation          isolation          `json:"isolation"`
	NonTargetReason    string             `json:"nonTargetReason,omitempty"`
	Cases              []benchmarkCase    `json:"cases"`
	RequiredProtocols  []string           `json:"requiredProtocols"`
	RequiredTargets    []string           `json:"requiredTargets,omitempty"`
	ObservedTargets    []string           `json:"observedTargets,omitempty"`
	MissingTargets     []string           `json:"missingTargets,omitempty"`
	NonTargets         []catalogNonTarget `json:"nonTargets,omitempty"`
}

type isolation struct {
	Home        string `json:"home"`
	Cache       string `json:"cache"`
	Output      string `json:"output"`
	InstallRoot string `json:"installRoot,omitempty"`
}

type benchmarkCase struct {
	ID             string     `json:"id"`
	StableKey      string     `json:"stableKey"`
	Source         string     `json:"source"`
	Fixture        string     `json:"fixture,omitempty"`
	Command        string     `json:"command"`
	Args           []string   `json:"args"`
	ExpectedKind   string     `json:"expectedKind"`
	DetectedTarget string     `json:"detectedTarget,omitempty"`
	Setup          [][]string `json:"setup,omitempty"`
}

func main() {
	fixturesPath := flag.String("fixtures", "fixtures/golden/manifest.json", "golden fixture manifest")
	inputsPath := flag.String("inputs", "tools/benchmark/target-inputs.json", "real target input catalog; missing file is allowed")
	flag.Parse()
	var fixtures fixtureManifest
	data, err := os.ReadFile(*fixturesPath)
	if err != nil {
		fatal(err)
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		fatal(fmt.Errorf("parse %s: %w", *fixturesPath, err))
	}
	var inputs inputCatalog
	if inputData, readErr := os.ReadFile(*inputsPath); readErr == nil {
		if err := json.Unmarshal(inputData, &inputs); err != nil {
			fatal(fmt.Errorf("parse %s: %w", *inputsPath, err))
		}
	} else if !os.IsNotExist(readErr) {
		fatal(readErr)
	}
	result := buildMatrixWithInputs(app.NewRootCommand(), fixtures, inputs)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fatal(err)
	}
}

func buildMatrix(root *cobra.Command, fixtures fixtureManifest) matrix {
	return buildMatrixWithInputs(root, fixtures, inputCatalog{})
}

func buildMatrixWithInputs(root *cobra.Command, fixtures fixtureManifest, inputs inputCatalog) matrix {
	paths := collectLeafPaths(root)
	result := matrix{
		Schema: schema,
		Protocols: []protocol{
			{Name: "independent-cold", FreshHome: true, CacheDelta: "record"},
			{Name: "shared-build-cold", SharedHome: true, CacheDelta: "record"},
			{Name: "warm", SharedHome: true, CacheDelta: "record"},
			{Name: "repeat-warm", SharedHome: true, CacheDelta: "must-be-zero"},
		},
		Targets: []target{
			{Name: "retail-cn", Source: "remote", Region: "cn", Product: "wow", Build: retailCNBuild, Locale: "zhCN"},
			{Name: "classic-era-us", Source: "remote", Region: "us", Product: "wow_classic_era", Build: classicEraUSBuild, Locale: "enUS"},
		},
	}
	protocolNames := make([]string, len(result.Protocols))
	for i := range result.Protocols {
		protocolNames[i] = result.Protocols[i].Name
	}
	for _, path := range paths {
		entry := commandBenchmark{
			Path: path, Risk: riskFor(path), TargetMode: targetModeFor(path), OutputComparison: outputComparisonFor(path), RepeatWarmCache: repeatWarmCacheFor(path), BaselineComparison: baselineComparisonFor(path), GoldenComparison: goldenComparisonFor(path), Execution: "execute",
			Isolation:         isolation{Home: "${RUN_ROOT}/${PROTOCOL}/${CASE_ID}/home", Cache: "${WOWDATA_HOME}/cache", Output: "${RUN_ROOT}/${PROTOCOL}/${CASE_ID}/output"},
			RequiredProtocols: append([]string(nil), protocolNames...),
		}
		if entry.Risk == "install-destructive" {
			entry.Execution = "record-only"
			entry.Isolation.InstallRoot = "${RUN_ROOT}/${PROTOCOL}/${CASE_ID}/install"
			entry.NonTargetReason = "execute only in disposable HOME, cache, npm prefix, and install root; Build matrix generation does not mutate the active installation"
		}
		for _, candidate := range fixtures.Fixtures {
			if !containsWords(normalizeFixtureArgs(candidate), strings.Fields(path)) {
				continue
			}
			args := stripAutoWarmupArgs(fixtureExecutionArgs(candidate))
			entry.Cases = append(entry.Cases, withStableKey(path, benchmarkCase{
				ID: slug(path + "-" + candidate.Name), Source: "golden-fixture", Fixture: candidate.Name,
				Command: "wowdata", Args: args, ExpectedKind: candidate.Kind,
				DetectedTarget: detectTarget(candidate.Args),
			}))
			result.Summary.FixtureCaseCount++
		}
		if entry.Risk == "install-destructive" {
			entry.Cases = append(entry.Cases, withStableKey(path, installExecutionCase(path)))
			result.Summary.GeneratedSmoke++
		}
		for _, configured := range inputs.Cases {
			if configured.Path != path {
				continue
			}
			expected := configured.ExpectedKind
			if expected == "" {
				expected = "success"
			}
			args, detectedTarget := materializeCatalogTarget(configured)
			entry.Cases = append(entry.Cases, withStableKey(path, benchmarkCase{
				ID: configured.ID, Source: "target-input-catalog", Command: "wowdata", Args: args,
				ExpectedKind: expected, DetectedTarget: detectedTarget, Setup: configured.Setup,
			}))
		}
		if len(entry.Cases) == 0 {
			entry.Cases = []benchmarkCase{withStableKey(path, generatedCase(path, entry.Execution))}
			result.Summary.GeneratedSmoke++
		} else {
			result.Summary.Behavioral++
		}
		if entry.Execution == "record-only" {
			result.Summary.RecordOnly++
		}
		if entry.TargetMode == "retail-cn-and-classic-era-us" {
			entry.RequiredTargets = []string{"retail-cn", "classic-era-us"}
			observed := make(map[string]bool)
			for _, candidate := range entry.Cases {
				if candidate.DetectedTarget != "" {
					observed[candidate.DetectedTarget] = true
				}
			}
			for _, required := range entry.RequiredTargets {
				if observed[required] {
					entry.ObservedTargets = append(entry.ObservedTargets, required)
				} else if exclusion, ok := findNonTarget(inputs.NonTargets, path, required); ok {
					entry.NonTargets = append(entry.NonTargets, exclusion)
					result.Summary.NonTargetExclusions++
				} else {
					entry.MissingTargets = append(entry.MissingTargets, required)
					result.Summary.TargetCoverageGaps++
				}
			}
		}
		result.Summary.ProtocolCaseCount += len(entry.Cases) * len(entry.RequiredProtocols)
		result.Commands = append(result.Commands, entry)
	}
	result.Summary.LeafCommands = len(result.Commands)
	return result
}

// Wildcard catalog cases are portable only at the source-catalog level. The
// executable matrix runs in isolated homes, so materialize them against the
// fixed Classic Era corpus used by the corresponding golden evidence.
func materializeCatalogTarget(candidate catalogCase) ([]string, string) {
	args := stripAutoWarmupArgs(candidate.Args)
	if candidate.Target == "*" {
		prefix := []string{"--source", "remote", "--region", "us", "--product", "wow_classic_era", "--build", classicEraUSBuild, "--locale", "enUS"}
		return append(prefix, args...), "classic-era-us"
	}
	if candidate.Source == "remote" && candidate.Target == "retail-cn" {
		prefix := []string{"--source", "remote", "--region", "cn", "--product", "wow", "--build", retailCNBuild, "--locale", "zhCN"}
		return appendMissingTargetArgs(prefix, args), candidate.Target
	}
	if candidate.Source == "local" && strings.HasPrefix(candidate.Target, "retail-cn-local-") {
		build := strings.TrimPrefix(candidate.Target, "retail-cn-local-")
		prefix := []string{"--source", "local", "--region", "cn", "--product", "wow", "--build", build, "--locale", "zhCN", "--path", "${WOWDATA_GAME_DIR}"}
		return appendMissingTargetArgs(prefix, args), candidate.Target
	}
	return args, candidate.Target
}

func appendMissingTargetArgs(prefix, args []string) []string {
	missing := make([]string, 0, len(prefix))
	for i := 0; i+1 < len(prefix); i += 2 {
		name, value := prefix[i], prefix[i+1]
		if hasArgument(args, name) {
			continue
		}
		missing = append(missing, name, value)
	}
	return append(missing, args...)
}

func withStableKey(path string, value benchmarkCase) benchmarkCase {
	canonical := struct {
		Path           string     `json:"path"`
		Command        string     `json:"command"`
		Args           []string   `json:"args"`
		Setup          [][]string `json:"setup,omitempty"`
		ExpectedKind   string     `json:"expectedKind"`
		DetectedTarget string     `json:"detectedTarget,omitempty"`
	}{path, value.Command, value.Args, value.Setup, value.ExpectedKind, value.DetectedTarget}
	data, _ := json.Marshal(canonical)
	value.StableKey = fmt.Sprintf("%x", sha256.Sum256(data))
	return value
}

func findNonTarget(values []catalogNonTarget, path, target string) (catalogNonTarget, bool) {
	for _, value := range values {
		if value.Path == path && value.Target == target && value.Reason != "" && value.Evidence != "" {
			return value, true
		}
	}
	return catalogNonTarget{}, false
}

func collectLeafPaths(root *cobra.Command) []string {
	var paths []string
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		children := command.Commands()
		if len(children) == 0 {
			paths = append(paths, strings.TrimPrefix(command.CommandPath(), root.Name()+" "))
			return
		}
		for _, child := range children {
			walk(child)
		}
	}
	walk(root)
	sort.Strings(paths)
	return paths
}

func generatedCase(path, execution string) benchmarkCase {
	args := strings.Fields(path)
	result := benchmarkCase{ID: slug(path + "-generated"), Source: "generated-isolated-smoke", Command: "wowdata", ExpectedKind: "success"}
	switch path {
	case "profile set":
		result.Args = append(args, "benchmark-profile", "--source", "remote", "--region", "cn", "--product", "wow", "--build", retailCNBuild, "--locale", "zhCN")
	case "profile show":
		result.Args = append(args, "benchmark-profile")
		result.Setup = [][]string{{"profile", "set", "benchmark-profile", "--source", "remote", "--region", "cn", "--product", "wow", "--build", retailCNBuild, "--locale", "zhCN"}}
	case "profile remove":
		result.Args = append(args, "benchmark-profile")
		result.Setup = [][]string{{"profile", "set", "benchmark-profile", "--source", "remote", "--region", "cn", "--product", "wow", "--build", retailCNBuild, "--locale", "zhCN"}}
	case "golden capture", "golden compare":
		result.Args = append(args, "--help")
		result.Source = "generated-help-regression"
	default:
		result.Args = args
	}
	if execution == "record-only" {
		result.Args = append(args, "--help")
		result.Source = "generated-isolated-record-plan"
	}
	return result
}

func installExecutionCase(path string) benchmarkCase {
	args := strings.Fields(path)
	if path == "update" {
		args = append(args, "--version", "latest")
	}
	return benchmarkCase{ID: slug(path + "-isolated-execution"), Source: "generated-isolated-install", Command: "wowdata", Args: args, ExpectedKind: "success"}
}

func riskFor(path string) string {
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

func outputComparisonFor(path string) string {
	root := strings.Fields(path)[0]
	if root == "cache" || root == "profile" || path == "update" || path == "uninstall" {
		return "stable-within-protocol"
	}
	return "strict-cross-protocol"
}

func repeatWarmCacheFor(path string) string {
	if path == "cache clear" {
		return "expected-mutation"
	}
	return "stable"
}

func baselineComparisonFor(path string) string {
	if path == "encounter export" {
		return "unsupported-or-strict"
	}
	return "strict"
}

func goldenComparisonFor(path string) string {
	if outputComparisonFor(path) == "stable-within-protocol" {
		return "independent-cold-only"
	}
	return "all-protocols"
}

func targetModeFor(path string) string {
	prefix := strings.Fields(path)[0]
	switch prefix {
	case "db2", "spell", "encounter", "file", "icon", "item", "creature", "decor", "warmup":
		return "retail-cn-and-classic-era-us"
	case "casc":
		if path != "casc products" {
			return "retail-cn-and-classic-era-us"
		}
	}
	return "target-neutral"
}

func normalizeFixtureArgs(value fixture) []string {
	args := value.Args
	if value.Command == "go" && len(args) >= 2 && args[0] == "run" && filepathSlash(args[1]) == "./cmd/wowdata" {
		return args[2:]
	}
	return args
}

func fixtureExecutionArgs(value fixture) []string {
	args := append([]string(nil), normalizeFixtureArgs(value)...)
	if !hasArgument(args, "--auto-warmup") {
		return args
	}
	if !hasArgument(args, "--locale") {
		locale := "enUS"
		if argumentValue(args, "--region") == "cn" {
			locale = "zhCN"
		}
		args = append(args, "--locale", locale)
	}
	return args
}

func stripAutoWarmupArgs(args []string) []string {
	result := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--auto-warmup" || strings.HasPrefix(arg, "--auto-warmup=") {
			continue
		}
		result = append(result, arg)
	}
	return result
}

func hasArgument(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

func detectTarget(args []string) string {
	region, product, locale := argumentValue(args, "--region"), argumentValue(args, "--product"), argumentValue(args, "--locale")
	if region == "cn" && product == "wow" && (locale == "" || locale == "zhCN") {
		return "retail-cn"
	}
	if region == "us" && product == "wow_classic_era" && (locale == "" || locale == "enUS") {
		return "classic-era-us"
	}
	if region != "" || product != "" || locale != "" {
		return strings.Trim(strings.Join([]string{region, product, locale}, "/"), "/")
	}
	return ""
}

func argumentValue(args []string, name string) string {
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, name+"=") {
			return strings.TrimPrefix(arg, name+"=")
		}
	}
	return ""
}

func containsWords(args, words []string) bool {
	for start := 0; start+len(words) <= len(args); start++ {
		matched := len(words) > 0
		for i := range words {
			if args[start+i] != words[i] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func slug(value string) string {
	value = strings.ToLower(value)
	var output strings.Builder
	dash := false
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			output.WriteRune(r)
			dash = false
		} else if !dash && output.Len() > 0 {
			output.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(output.String(), "-")
}

func filepathSlash(value string) string { return strings.ReplaceAll(value, "\\", "/") }

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
