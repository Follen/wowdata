package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testFloorRates() matrixFloorRates {
	return matrixFloorRates{
		processStart: 10, metadataTTFB: 100, largeTTFB: 200,
		metadataBody: 1000, largeBody: 2000,
		work: map[string]workCalibration{
			"wdc-row\x00rows":      {class: "wdc-row", unit: "rows", resource: "cpu", rate: 100},
			"file-write\x00bytes":  {class: "file-write", unit: "bytes", resource: "disk", rate: 3000},
			"json-encode\x00bytes": {class: "json-encode", unit: "bytes", resource: "cpu", rate: 2000},
		},
	}
}

func TestBuildMatrixFloorInputUsesInstrumentedWorkAndExplicitDAG(t *testing.T) {
	dir := t.TempDir()
	stdout := filepath.Join(dir, "stdout.log")
	stderr := filepath.Join(dir, "stderr.log")
	if err := os.WriteFile(stdout, []byte("result\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := strings.Join([]string{
		"timing stage=casc-build-config duration=9s total=9s requests=2 uniqueBytes=100 duplicateBytes=0 canceled=0",
		"timing stage=casc-root-selected duration=8s total=17s requests=4 uniqueBytes=900 duplicateBytes=0 canceled=0",
	}, "\n")
	if err := os.WriteFile(stderr, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	resource := &matrixResourceMetrics{
		Schema:            "wowdata.resource-metrics.v2",
		PoolCapacity:      map[string]int{"metadata": 2, "large-range": 4},
		StageDependencies: map[string][]string{"casc-build-config": {}, "casc-root-selected": {"casc-build-config"}},
		Work:              matrixResourceWork{Complete: true, Counters: []matrixWorkCounter{{Class: "wdc-row", Unit: "rows", Units: 25}, {Class: "file-write", Unit: "bytes", Units: 50}, {Class: "json-encode", Unit: "bytes", Units: 7}}},
	}
	samples := []matrixFloorSample{{
		CommandPath: "file encoding", CaseID: "case", StableCaseKey: "stable", Protocol: "independent-cold", Status: "pass",
		ExitCode: 0, WallMilliseconds: 10000, CPUMilliseconds: 250, Stdout: stdout,
		NetworkMetrics: &matrixNetworkMetrics{UniquePayloadBytes: 1000}, ResourceMetrics: resource,
	}}
	in, audit, reason := buildMatrixFloorInput(samples, testFloorRates())
	if reason != "" {
		t.Fatal(reason)
	}
	first, err := calculate(in)
	if err != nil {
		t.Fatal(err)
	}
	samples[0].CPUMilliseconds = 999999
	mutated, _, reason := buildMatrixFloorInput(samples, testFloorRates())
	if reason != "" {
		t.Fatal(reason)
	}
	second, err := calculate(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if first.FloorNanos != second.FloorNanos {
		t.Fatalf("CPU observation changed floor: %d != %d", first.FloorNanos, second.FloorNanos)
	}
	if !strings.Contains(audit.WorkMeaning, "process CPU time is observation only") || len(audit.WorkCounters) != 3 {
		t.Fatalf("audit=%+v", audit)
	}
	var root node
	for _, current := range in.Nodes {
		if current.ID == "casc-root-selected" {
			root = current
		}
	}
	if len(root.DependsOn) != 1 || root.DependsOn[0] != "casc-build-config" || root.SharedNetworkResourceClass != "cdn-shared" {
		t.Fatalf("root DAG=%+v", root)
	}
}

func TestBuildMatrixFloorInputMarksIncompleteWorkMissing(t *testing.T) {
	dir := t.TempDir()
	stdout := filepath.Join(dir, "stdout.log")
	if err := os.WriteFile(stdout, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, reason := buildMatrixFloorInput([]matrixFloorSample{{
		Status: "pass", ExitCode: 0, Stdout: stdout,
		ResourceMetrics: &matrixResourceMetrics{Schema: "wowdata.resource-metrics.v2", Work: matrixResourceWork{Complete: false}},
	}}, testFloorRates())
	if !strings.Contains(reason, "incomplete") {
		t.Fatalf("reason=%q", reason)
	}
}

func TestRequiredOutputClassesDistinguishesImageAndBinary(t *testing.T) {
	png := requiredOutputClasses(matrixFloorSample{CommandPath: "icon export", Args: []string{"--output", "x.png"}})
	if !containsString(png, "json-encode") || !containsString(png, "file-write") || !containsString(png, "png-encode") {
		t.Fatalf("png classes=%v", png)
	}
	binary := requiredOutputClasses(matrixFloorSample{CommandPath: "file export", Args: []string{"--output=x.bin"}})
	if !containsString(binary, "file-write") || containsString(binary, "png-encode") {
		t.Fatalf("binary classes=%v", binary)
	}
}

func TestLoadMatrixFloorRatesReadsStorageCalibrations(t *testing.T) {
	dir := t.TempDir()
	local, network, process := filepath.Join(dir, "local.json"), filepath.Join(dir, "network.json"), filepath.Join(dir, "process.json")
	write := func(path string, value any) {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(local, map[string]any{
		"disk":    map[string]any{"sequentialReadBytesPerSecond": 1, "sequentialWriteBytesPerSecond": 2},
		"compute": []map[string]any{{"resourceClass": "json-encode", "workUnit": "bytes", "unitsPerSecond": 3}},
		"storage": []map[string]any{{"resourceClass": "file-read", "workUnit": "bytes", "unitsPerSecond": 11}, {"resourceClass": "file-write", "workUnit": "bytes", "unitsPerSecond": 22}},
	})
	write(network, map[string]any{"samples": []map[string]any{{"name": "cdn-small-24", "responseBytes": 10, "bodyNanos": 10, "ttfbNanos": 1}, {"name": "cdn-multi", "responseBytes": 20, "bodyNanos": 10, "ttfbNanos": 2}}})
	write(process, map[string]any{"p50Nanos": 1})
	rates, err := loadMatrixFloorRates(matrixGenerationConfig{LocalCalibration: local, NetworkCalibration: network, ProcessCalibration: process})
	if err != nil {
		t.Fatal(err)
	}
	if rates.work["file-read\x00bytes"].rate != 11 || rates.work["file-write\x00bytes"].rate != 22 {
		t.Fatalf("storage rates=%+v", rates.work)
	}
}

func TestValidateFloorMatrixRunRequiresCompleteFormalMatrix(t *testing.T) {
	run := matrixFloorRun{Schema: "wowdata.command-matrix-run.v1", RunID: "run", MatrixSha256: "matrix", Status: "pass", Formal: true, FormalProtocolComplete: true, Repetitions: 10, MatrixCases: formalMatrixCases, SelectedCases: formalMatrixCases}
	match := true
	for caseIndex := 0; caseIndex < formalMatrixCases; caseIndex++ {
		for _, protocol := range formalProtocols {
			for repetition := 1; repetition <= run.Repetitions; repetition++ {
				run.Samples = append(run.Samples, matrixFloorSample{CommandPath: "command", StableCaseKey: string(rune(caseIndex + 1)), Protocol: protocol, Status: "pass", ExitCode: 0, Repetition: repetition, RunID: run.RunID, MatrixSha256: run.MatrixSha256, Baseline: &matrixFloorBaseline{OutputMatch: &match}})
			}
		}
	}
	if err := validateFloorMatrixRun(run); err != nil {
		t.Fatal(err)
	}
	run.Formal = false
	if err := validateFloorMatrixRun(run); err == nil || !strings.Contains(err.Error(), "formal") {
		t.Fatalf("error=%v", err)
	}
}
