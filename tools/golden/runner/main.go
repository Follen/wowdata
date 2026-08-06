package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"wowdata/internal/golden"
)

var requiredGroups = []string{
	"casc", "creature", "db2", "decor", "encounter", "errors", "file", "icon",
	"item", "spell", "video", "warmup", "profile", "cache", "doctor", "update", "uninstall",
}

var fixedBuildPattern = regexp.MustCompile(`^(?:[0-9]+(?:\.[0-9]+)*|[0-9a-fA-F]{32})$`)

type capture struct {
	Name       string   `json:"name"`
	Command    string   `json:"command"`
	Args       []string `json:"args"`
	Comparison string   `json:"comparison,omitempty"`
	Isolation  string   `json:"isolation,omitempty"`
	ExitCode   int      `json:"exitCode"`
	Stdout     string   `json:"stdout"`
	Stderr     string   `json:"stderr"`
}

type targetIdentity struct {
	Source   string `json:"source"`
	Region   string `json:"region"`
	Product  string `json:"product"`
	Build    string `json:"build"`
	BuildKey string `json:"buildKey,omitempty"`
	Locale   string `json:"locale"`
}

type report struct {
	GeneratedAt       string                 `json:"generatedAt"`
	Manifest          string                 `json:"manifest"`
	CaptureCount      int                    `json:"captureCount"`
	PresentGroups     []string               `json:"presentGroups"`
	MissingGroups     []string               `json:"missingGroups"`
	FailedComparisons int                    `json:"failedComparisons"`
	Comparisons       []golden.CompareResult `json:"comparisons"`
	TargetIdentities  []targetIdentity       `json:"targetIdentities"`
	Compare           json.RawMessage        `json:"compare,omitempty"`
	Status            string                 `json:"status"`
}

func main() {
	manifestPath := flag.String("manifest", "fixtures/golden/manifest.json", "manifest path")
	outputRoot := flag.String("output", "analyze/golden", "actual output root")
	reportPath := flag.String("report", "analyze/golden/report.json", "report path")
	timeout := flag.Duration("timeout", 3*time.Minute, "per-command timeout")
	skipCompare := flag.Bool("skip-compare", false, "skip final compare command")
	reuseActual := flag.Bool("reuse-actual", false, "reuse existing actual captures")
	binaryOverride := flag.String("binary", "", "existing wowdata binary to execute")
	compareRoot := flag.String("compare-root", "fixtures/golden/go", "expected capture root")
	homeOverride := flag.String("home", "", "isolated WOWDATA_HOME; defaults to <output>/home")
	flag.Parse()
	code, err := run(*manifestPath, *outputRoot, *reportPath, *timeout, *skipCompare, *reuseActual, *binaryOverride, *compareRoot, *homeOverride)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
}

func run(manifestPath, outputRoot, reportPath string, timeout time.Duration, skipCompare, reuseActual bool, binaryOverride, compareRoot, homeOverride string) (int, error) {
	captureRoot := filepath.Join("fixtures", "golden", "go")
	paths := make([]string, 0)
	err := filepath.WalkDir(captureRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".json") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return 1, err
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return 1, fmt.Errorf("no golden captures found under %s", captureRoot)
	}
	binaryPath := binaryOverride
	if binaryPath == "" {
		binaryName := "wowdata-golden"
		if runtime.GOOS == "windows" {
			binaryName += ".exe"
		}
		binaryPath = filepath.Join(outputRoot, binaryName)
	}
	binaryPath, err = filepath.Abs(binaryPath)
	if err != nil {
		return 1, err
	}
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0755); err != nil {
		return 1, err
	}
	isolatedHome := homeOverride
	if isolatedHome == "" {
		isolatedHome = filepath.Join(outputRoot, "home")
	}
	isolatedHome, err = filepath.Abs(isolatedHome)
	if err != nil {
		return 1, err
	}
	if err := os.Setenv("WOWDATA_HOME", isolatedHome); err != nil {
		return 1, err
	}
	if binaryOverride == "" {
		build := exec.Command("go", "build", "-trimpath", "-o", binaryPath, "./cmd/wowdata")
		if output, err := build.CombinedOutput(); err != nil {
			return 1, fmt.Errorf("build golden binary: %w: %s", err, output)
		}
	}
	manifest := golden.Manifest{Version: 2, Source: "tools/golden/runner", RequiredGroups: requiredGroups}
	comparisonResults := make([]golden.CompareResult, 0, len(paths))
	failedComparisons := 0
	for _, group := range requiredGroups {
		if group != "errors" {
			manifest.RequiredSuccessGroups = append(manifest.RequiredSuccessGroups, group)
		}
	}
	present := make(map[string]bool)
	identities := make(map[string]targetIdentity)
	discoveredBuildKeys := make(map[string]string)
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return 1, readErr
		}
		var expected capture
		if err := json.Unmarshal(data, &expected); err != nil {
			return 1, fmt.Errorf("parse %s: %w", path, err)
		}
		if err := validateVersionedCapture(expected); err != nil {
			return 1, fmt.Errorf("validate %s: %w", path, err)
		}
		for key, buildKey := range capturedProductBuildKeys(expected) {
			discoveredBuildKeys[key] = buildKey
		}
		identity, targeted, err := validateCaptureTarget(expected)
		if err != nil {
			return 1, fmt.Errorf("validate %s: %w", path, err)
		}
		if targeted {
			key := strings.Join([]string{identity.Source, identity.Region, identity.Product, identity.Build, identity.Locale}, "\x00")
			if buildKey := capturedBuildKey(expected); buildKey != "" {
				identity.BuildKey = buildKey
			}
			if previous := identities[key]; previous.BuildKey != "" && identity.BuildKey == "" {
				identity.BuildKey = previous.BuildKey
			}
			identities[key] = identity
		}
		relative, err := filepath.Rel(captureRoot, path)
		if err != nil || strings.HasPrefix(relative, "..") {
			return 1, fmt.Errorf("capture escaped root: %s", path)
		}
		actualPath := filepath.Join(outputRoot, relative)
		captureHome := isolatedHome
		if expected.Isolation == "fresh-home" {
			captureHome, err = freshCaptureHome(outputRoot, expected.Name)
			if err != nil {
				return 1, err
			}
			if err := os.RemoveAll(captureHome); err != nil {
				return 1, fmt.Errorf("reset isolated home for %s: %w", expected.Name, err)
			}
		}
		var actual capture
		if reuseActual {
			actualData, readErr := os.ReadFile(actualPath)
			if readErr != nil {
				return 1, fmt.Errorf("reuse actual %s: %w", actualPath, readErr)
			}
			if err := json.Unmarshal(actualData, &actual); err != nil {
				return 1, fmt.Errorf("parse actual %s: %w", actualPath, err)
			}
		} else {
			actual, err = executeCapture(expected, timeout, binaryPath, captureHome)
			if err != nil {
				return 1, fmt.Errorf("execute %s: %w", expected.Name, err)
			}
			actual = normalizeCaptureHome(actual, captureHome)
			if err := writeJSON(actualPath, actual); err != nil {
				return 1, err
			}
		}
		expectedPath := filepath.Join(compareRoot, relative)
		expectedData, err := os.ReadFile(expectedPath)
		if err != nil {
			return 1, fmt.Errorf("read expected %s: %w", expectedPath, err)
		}
		var comparisonExpected capture
		if err := json.Unmarshal(expectedData, &comparisonExpected); err != nil {
			return 1, fmt.Errorf("parse expected %s: %w", expectedPath, err)
		}
		comparisonExpected = normalizeCaptureHome(comparisonExpected, captureHome)
		expectedData, err = json.Marshal(comparisonExpected)
		if err != nil {
			return 1, err
		}
		actualData, err := json.Marshal(actual)
		if err != nil {
			return 1, err
		}
		comparison, compareErr := golden.CompareJSONWithMode(expectedData, actualData, expected.Comparison)
		comparison.Fixture = filepath.ToSlash(expectedPath)
		if compareErr != nil || !comparison.Equal {
			failedComparisons++
		}
		comparisonResults = append(comparisonResults, comparison)
		group := strings.Split(filepath.ToSlash(relative), "/")[0]
		present[group] = true
		kind := "error"
		if comparisonExpected.ExitCode == 0 {
			kind = "success"
		}
		manifest.Fixtures = append(manifest.Fixtures, golden.ManifestEntry{
			Name: expected.Name, Group: group, Kind: kind, Command: expected.Command,
			Args: expected.Args, Comparison: expected.Comparison, Source: identity.Source, Region: identity.Region,
			Product: identity.Product, Build: identity.Build, Fixture: filepath.ToSlash(expectedPath), Actual: filepath.ToSlash(actualPath),
		})
	}
	if err := writeJSON(manifestPath, manifest); err != nil {
		return 1, err
	}
	presentGroups := make([]string, 0, len(present))
	missing := make([]string, 0)
	for _, group := range requiredGroups {
		if present[group] {
			presentGroups = append(presentGroups, group)
		} else {
			missing = append(missing, group)
		}
	}
	targetIdentities := make([]targetIdentity, 0, len(identities))
	for _, identity := range identities {
		if identity.BuildKey == "" {
			identity.BuildKey = discoveredBuildKeys[identity.Product+"\x00"+identity.Build]
		}
		targetIdentities = append(targetIdentities, identity)
	}
	sort.Slice(targetIdentities, func(i, j int) bool {
		left, right := targetIdentities[i], targetIdentities[j]
		return strings.Join([]string{left.Source, left.Region, left.Product, left.Build, left.Locale}, "\x00") <
			strings.Join([]string{right.Source, right.Region, right.Product, right.Build, right.Locale}, "\x00")
	})
	result := report{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), Manifest: filepath.ToSlash(manifestPath),
		CaptureCount: len(paths), PresentGroups: presentGroups, MissingGroups: missing,
		FailedComparisons: failedComparisons, Comparisons: comparisonResults, TargetIdentities: targetIdentities, Status: "pass",
	}
	compareFailed := false
	if !skipCompare {
		compareCapture, err := executeCapture(capture{Command: "wowdata", Args: []string{"golden", "compare", "--all", "--fixture", manifestPath}}, timeout, binaryPath, isolatedHome)
		if err != nil {
			return 1, err
		}
		compareJSON := []byte(strings.TrimSpace(compareCapture.Stdout))
		if json.Valid(compareJSON) {
			result.Compare = json.RawMessage(compareJSON)
		}
		compareFailed = compareCapture.ExitCode != 0
	}
	if len(missing) > 0 {
		result.Status = "coverage-gap"
	} else if compareFailed || failedComparisons > 0 {
		result.Status = "failed"
	}
	if err := writeJSON(reportPath, result); err != nil {
		return 1, err
	}
	if len(missing) > 0 {
		return 2, fmt.Errorf("golden coverage gaps: %s", strings.Join(missing, ", "))
	}
	if compareFailed {
		return 1, fmt.Errorf("golden comparison failed")
	}
	if failedComparisons > 0 {
		return 1, fmt.Errorf("%d golden comparisons failed", failedComparisons)
	}
	return 0, nil
}

func freshCaptureHome(outputRoot, name string) (string, error) {
	home := filepath.Join(outputRoot, "state-homes", strings.ReplaceAll(filepath.ToSlash(name), "/", "-"))
	home, err := filepath.Abs(home)
	if err != nil {
		return "", fmt.Errorf("resolve fresh home for %s: %w", name, err)
	}
	return home, nil
}

func normalizeCaptureHome(value capture, home string) capture {
	if home == "" {
		return value
	}
	forms := []string{filepath.Clean(home), filepath.ToSlash(filepath.Clean(home))}
	for _, form := range forms {
		value.Stdout = strings.ReplaceAll(value.Stdout, form, "<WOWDATA_HOME>")
		value.Stderr = strings.ReplaceAll(value.Stderr, form, "<WOWDATA_HOME>")
	}
	return value
}

func executeCapture(input capture, timeout time.Duration, binaryPath, home string) (capture, error) {
	command := input.Command
	args := legacyTargetDefaults(input.Args)
	if command == "wowdata" {
		command = binaryPath
	} else if command == "go" && len(args) >= 2 && args[0] == "run" && filepath.ToSlash(args[1]) == "./cmd/wowdata" {
		command = binaryPath
		args = args[2:]
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	if home != "" {
		cmd.Env = append(os.Environ(), "WOWDATA_HOME="+home)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			exitCode = 124
			stderr.WriteString(fmt.Sprintf("command timed out after %s\n", timeout))
		} else if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return capture{}, err
		}
	}
	return capture{Name: input.Name, Command: input.Command, Args: input.Args, Comparison: input.Comparison, Isolation: input.Isolation, ExitCode: exitCode, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

func legacyTargetDefaults(input []string) []string {
	args := append([]string(nil), input...)
	if !hasArgument(args, "--auto-warmup") {
		return args
	}
	if !hasArgument(args, "--locale") {
		locale := "enUS"
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--region" && strings.EqualFold(args[i+1], "cn") {
				locale = "zhCN"
				break
			}
		}
		args = append(args, "--locale", locale)
	}
	return args
}

func validateCaptureTarget(value capture) (targetIdentity, bool, error) {
	args := value.Args
	identity := targetIdentity{
		Source: argumentValue(args, "--source"), Region: argumentValue(args, "--region"),
		Product: argumentValue(args, "--product"), Build: argumentValue(args, "--build"),
		Locale: argumentValue(args, "--locale"),
	}
	if !strings.EqualFold(identity.Source, "remote") || identity.Product == "" {
		return identity, false, nil
	}
	if identity.Build == "" {
		return identity, true, fmt.Errorf("remote target capture %q requires explicit --build", value.Name)
	}
	if argumentCount(args, "--build") != 1 {
		return identity, true, fmt.Errorf("remote target capture %q requires exactly one --build", value.Name)
	}
	if strings.EqualFold(identity.Build, "latest") {
		return identity, true, fmt.Errorf("remote target capture %q must pin --build instead of latest", value.Name)
	}
	if !fixedBuildPattern.MatchString(identity.Build) {
		return identity, true, fmt.Errorf("remote target capture %q has invalid fixed --build %q", value.Name, identity.Build)
	}
	if identity.Locale == "" {
		identity.Locale = inferredLocale(identity.Region)
	}
	return identity, true, nil
}

func validateVersionedCapture(value capture) error {
	const maxStableStderrBytes = 64 << 10
	if len(value.Stderr) > maxStableStderrBytes {
		return fmt.Errorf("versioned capture %q stderr is %d bytes; keep raw telemetry under analyze", value.Name, len(value.Stderr))
	}
	for _, line := range strings.Split(strings.ReplaceAll(value.Stderr, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "download ") {
			return fmt.Errorf("versioned capture %q contains transient download telemetry", value.Name)
		}
	}
	return nil
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

func argumentCount(args []string, name string) int {
	count := 0
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			count++
		}
	}
	return count
}

func inferredLocale(region string) string {
	if strings.EqualFold(region, "cn") {
		return "zhCN"
	}
	return "enUS"
}

func capturedBuildKey(value capture) string {
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			BuildKey string `json:"buildKey"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(value.Stdout), &envelope); err != nil || !envelope.OK {
		return ""
	}
	return envelope.Data.BuildKey
}

func capturedProductBuildKeys(value capture) map[string]string {
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Products []struct {
				Product        string `json:"product"`
				Version        string `json:"version"`
				BuildConfigKey string `json:"buildConfigKey"`
			} `json:"products"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(value.Stdout), &envelope); err != nil || !envelope.OK {
		return nil
	}
	result := make(map[string]string, len(envelope.Data.Products))
	for _, product := range envelope.Data.Products {
		if product.Product != "" && product.Version != "" && product.BuildConfigKey != "" {
			result[product.Product+"\x00"+product.Version] = product.BuildConfigKey
		}
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

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
