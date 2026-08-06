package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateRequiresExplicitFormalEvidence(t *testing.T) {
	dir := t.TempDir()
	cfg := missingConfig(dir)
	_, err := generate(cfg, []string{"z command", "a command"})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("error=%v", err)
	}
}

func TestBuildCaseJoinsFloorByStableKeyAndProtocol(t *testing.T) {
	run := matrixRun{
		Schema: "wowdata.command-matrix-run.v1", GeneratedAt: "2026-01-01T00:00:00Z", Binary: "optimized.exe", BaselineBinary: "baseline.exe", BinarySha256: "abc",
		Samples: []matrixSample{{
			CommandPath: "file encoding", CaseID: "case", StableCaseKey: "stable", Protocol: "independent-cold", Status: "pass",
			WallMilliseconds: 200, PeakWorkingSetBytes: 1000, NetworkMetrics: &networkMetrics{Requests: 3},
			Baseline:    &baselineSample{WallMilliseconds: 300, PeakWorkingSetBytes: 1200, OutputMatch: boolPtr(true)},
			GoldenMatch: boolPtr(true), CrossProtocolMatch: boolPtr(true), RepeatWarmCacheStable: boolPtr(true),
		}},
	}
	floor := floorReport{Schema: "wowdata.command-case-floor.v1", StableCaseKey: "stable", Protocol: "independent-cold", FloorMilliseconds: 100, CriticalPathNanos: 90000000, CriticalPath: []string{"target", "network"}}
	floor.AggregateResourceFloor.Dominant = "network"
	c := buildCase([]selectedSample{{run: run, sample: run.Samples[0]}}, map[string]floorReport{floorJoinKey("stable", "independent-cold"): floor})
	metrics := c.Protocols["independent-cold"]
	if metrics.Efficiency.P50Ratio != 2 || metrics.Floor.Status != "available" || metrics.CriticalPath.Status != "available" {
		t.Fatalf("case=%+v", c)
	}
}

func TestRegressionRequiresWithinProtocolReferenceForStatefulCommands(t *testing.T) {
	run := matrixRun{BaselineBinary: "baseline.exe", Binary: "current.exe"}
	base := baselineSample{OutputMatch: boolPtr(true)}
	first := selectedSample{run: run, sample: matrixSample{
		Status: "pass", GoldenMatch: boolPtr(true), Baseline: &base,
		OutputComparisonPolicy: "stable-within-protocol", OutputComparisonMatch: boolPtr(true),
		OutputComparisonReferenceAvailable: boolPtr(false), RepeatWarmCacheStable: boolPtr(true),
	}}
	if got := regressionFor([]selectedSample{first}); got.Status != "missing" {
		t.Fatalf("first repetition status = %q, want missing: %+v", got.Status, got)
	}
	second := first
	second.sample.OutputComparisonReferenceAvailable = boolPtr(true)
	if got := regressionFor([]selectedSample{first, second}); got.Status != "pass" || got.WithinProtocol != "pass" {
		t.Fatalf("referenced status = %+v, want pass", got)
	}
}

func TestLatestSamplesNeverMixesCasesBinariesOrRevisions(t *testing.T) {
	run := matrixRun{
		Schema: "wowdata.command-matrix-run.v1", GeneratedAt: "2026-01-01T00:00:00Z", RunID: "run-1", Revision: "rev-1", BinarySha256: "bin-1",
		Samples: []matrixSample{
			{CommandPath: "db2 rows", CaseID: "a", StableCaseKey: "case-a", Protocol: "independent-cold", Repetition: 1, WallMilliseconds: 10},
			{CommandPath: "db2 rows", CaseID: "a", StableCaseKey: "case-a", Protocol: "independent-cold", Repetition: 2, WallMilliseconds: 20},
			{CommandPath: "db2 rows", CaseID: "b", StableCaseKey: "case-b", Protocol: "independent-cold", Repetition: 1, WallMilliseconds: 1000},
		},
	}
	selected := latestSamples([]matrixRun{run})["db2 rows"]
	if len(selected) != 2 {
		t.Fatalf("selected %d case groups, want both fixed inputs", len(selected))
	}
	caseA := selected[0]
	if len(caseA) != 2 {
		t.Fatalf("case A has %d samples, want two repetitions", len(caseA))
	}
	for _, sample := range caseA {
		if sample.sample.StableCaseKey != "case-a" || sample.run.BinarySha256 != "bin-1" || sample.run.Revision != "rev-1" {
			t.Fatalf("mixed identity: %+v / %+v", sample.sample, sample.run)
		}
	}
	metrics := metricsFor(caseA)
	if metrics.Optimized.Samples != 2 || metrics.Optimized.Max != 20 {
		t.Fatalf("metrics mixed heterogeneous inputs: %+v", metrics.Optimized)
	}
}

func TestLatestSamplesSelectsOneNewestRunAsAUnit(t *testing.T) {
	old := matrixRun{GeneratedAt: "2026-01-01T00:00:00Z", RunID: "old", Revision: "rev-old", BinarySha256: "bin-old", Samples: []matrixSample{
		{CommandPath: "file export", CaseID: "case", StableCaseKey: "stable", Protocol: "independent-cold", Repetition: 1, WallMilliseconds: 10},
		{CommandPath: "file export", CaseID: "case", StableCaseKey: "stable", Protocol: "warm", Repetition: 1, WallMilliseconds: 5},
	}}
	newer := matrixRun{GeneratedAt: "2026-01-02T00:00:00Z", RunID: "new", Revision: "rev-new", BinarySha256: "bin-new", Samples: []matrixSample{
		{CommandPath: "file export", CaseID: "case", StableCaseKey: "stable", Protocol: "independent-cold", Repetition: 1, WallMilliseconds: 12},
	}}
	selected := latestSamples([]matrixRun{old, newer})["file export"]
	if len(selected) != 1 || len(selected[0]) != 1 || selected[0][0].run.RunID != "new" || selected[0][0].sample.Protocol != "independent-cold" {
		t.Fatalf("samples were backfilled across runs: %+v", selected)
	}
}

func TestValidateFormalMatrixRequiresEveryCaseProtocolAndRepetition(t *testing.T) {
	run := matrixRun{Schema: "wowdata.command-matrix-run.v1", RunID: "run", MatrixSha256: "matrix", Status: "pass", Formal: true, FormalProtocolComplete: true, Repetitions: 10, MatrixCases: expectedFormalMatrixCases, SelectedCases: expectedFormalMatrixCases, Binary: "current.exe", BinarySha256: "current-sha", BaselineBinary: "baseline.exe", BaselineBinarySha256: "baseline-sha"}
	match := true
	for caseIndex := 0; caseIndex < expectedFormalMatrixCases; caseIndex++ {
		stable := string(rune(caseIndex + 1))
		for _, protocol := range requiredProtocols {
			for repetition := 1; repetition <= run.Repetitions; repetition++ {
				run.Samples = append(run.Samples, matrixSample{CommandPath: "command", CaseID: stable, StableCaseKey: stable, Protocol: protocol, Status: "pass", Repetition: repetition, RunID: run.RunID, MatrixSha256: run.MatrixSha256, BinarySha256: run.BinarySha256, Baseline: &baselineSample{OutputMatch: &match}})
			}
		}
	}
	if err := validateFormalMatrix(run, []string{"command"}); err != nil {
		t.Fatal(err)
	}
	run.Samples = run.Samples[:len(run.Samples)-1]
	if err := validateFormalMatrix(run, []string{"command"}); err == nil || !strings.Contains(err.Error(), "repetitions") {
		t.Fatalf("error=%v", err)
	}
}

func TestLoadFormalEvidenceJoinsCompleteMissingFloorIndex(t *testing.T) {
	dir := t.TempDir()
	matrixPath, indexPath := filepath.Join(dir, "matrix.json"), filepath.Join(dir, "floors.json")
	run := matrixRun{Schema: "wowdata.command-matrix-run.v1", RunID: "run", MatrixSha256: "matrix", Status: "pass", Formal: true, FormalProtocolComplete: true, Repetitions: 10, MatrixCases: expectedFormalMatrixCases, SelectedCases: expectedFormalMatrixCases, Binary: "current.exe", BinarySha256: "current-sha", BaselineBinary: "baseline.exe", BaselineBinarySha256: "baseline-sha"}
	index := floorIndex{Schema: "wowdata.command-floor-index.v1", RunID: run.RunID, MatrixSha256: run.MatrixSha256}
	match := true
	for caseIndex := 0; caseIndex < expectedFormalMatrixCases; caseIndex++ {
		stable := string(rune(caseIndex + 1))
		for _, protocol := range requiredProtocols {
			index.Cases = append(index.Cases, floorIndexCase{CommandPath: "command", CaseID: stable, StableCaseKey: stable, Protocol: protocol, Status: "missing", Reason: "instrumented work incomplete", Samples: 10})
			for repetition := 1; repetition <= run.Repetitions; repetition++ {
				run.Samples = append(run.Samples, matrixSample{CommandPath: "command", CaseID: stable, StableCaseKey: stable, Protocol: protocol, Status: "pass", Repetition: repetition, RunID: run.RunID, MatrixSha256: run.MatrixSha256, BinarySha256: run.BinarySha256, Baseline: &baselineSample{OutputMatch: &match}})
			}
		}
	}
	index.Summary.StableCaseProtocols, index.Summary.Missing = len(index.Cases), len(index.Cases)
	writeJSON := func(path string, value any) {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON(matrixPath, run)
	writeJSON(indexPath, index)
	loaded, floors, err := loadFormalEvidence(matrixPath, indexPath, []string{"command"})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RunID != run.RunID || len(floors) != expectedFormalMatrixCases*len(requiredProtocols) {
		t.Fatalf("run=%q floors=%d", loaded.RunID, len(floors))
	}
}

func TestBuildCommandRetainsEveryFixedInputWithoutMixingMetrics(t *testing.T) {
	run := matrixRun{RunID: "run", Revision: "rev", BinarySha256: "bin"}
	groups := [][]selectedSample{
		{{run: run, sample: matrixSample{CommandPath: "db2 rows", CaseID: "small", StableCaseKey: "a-small", Protocol: "independent-cold", Repetition: 1, WallMilliseconds: 10, Status: "pass"}}},
		{{run: run, sample: matrixSample{CommandPath: "db2 rows", CaseID: "large", StableCaseKey: "b-large", Protocol: "independent-cold", Repetition: 1, WallMilliseconds: 1000, Status: "pass"}}},
	}
	command := buildCommand("db2 rows", groups, map[string]floorReport{})
	if len(command.Cases) != 2 || command.Cases[0].CaseID != "small" || command.Cases[1].CaseID != "large" {
		t.Fatalf("fixed inputs were dropped: %+v", command.Cases)
	}
	if command.Cases[0].Optimized.Max != 10 || command.Cases[1].Optimized.Max != 1000 {
		t.Fatalf("fixed-input timings were mixed: %+v", command.Cases)
	}
	if command.Optimized.Status != "missing" || !strings.Contains(command.Optimized.Reason, "cases[]") {
		t.Fatalf("command aggregate should direct consumers to cases[]: %+v", command.Optimized)
	}
}

func TestMarkdownCallsOutUnclassifiedFloors(t *testing.T) {
	command := commandReport{
		Path: "x", Baseline: missingMetric("none"), Optimized: missingMetric("none"),
		Floor: floorStatus{Status: "missing", Reason: "none"}, Efficiency: efficiencyStatus{Status: "missing", Reason: "none"},
		Memory: missingMetric("none"), Requests: missingMetric("none"), Regression: missingRegression("none"),
		CriticalPath: criticalPathStatus{Status: "missing", Reason: "none"},
	}
	text := markdown(finalReport{Status: "draft", Commands: []commandReport{command}, UnclassifiedFloor: []string{"x"}})
	if !strings.Contains(text, "not a pass") || !strings.Contains(text, "missing command floor evidence") {
		t.Fatal(text)
	}
}

func TestExplicitEvidenceWinsRegardlessOfStatus(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "explicit.json")
	newer := filepath.Join(dir, "newer.json")
	if err := os.WriteFile(explicit, []byte(`{"schema":"example.v1","generatedAt":"2026-01-01T00:00:00Z","status":"fail"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte(`{"schema":"example.v1","generatedAt":"2027-01-01T00:00:00Z","status":"pass"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var audit []evidence
	if selected := selectEvidence(explicit, dir, "example.v1", &audit); selected != explicit {
		t.Fatalf("selected %q", selected)
	}
	if len(audit) != 1 || !strings.Contains(audit[0].Reason, "status did not influence") {
		t.Fatalf("audit=%+v", audit)
	}
}

func missingConfig(root string) config {
	return config{EvidenceRoot: root, LocalCalibration: "missing", NetworkCalibration: "missing", Corpus: "missing", Resume: "missing", MetadataTournament: "missing", LargeRangeTournament: "missing", ChunkTournament: "missing"}
}

func boolPtr(value bool) *bool { return &value }
