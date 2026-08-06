package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const matrixFloorIndexSchema = "wowdata.command-floor-index.v1"
const matrixCaseFloorSchema = "wowdata.command-case-floor.v1"
const formalMatrixCases = 91

var formalProtocols = []string{"independent-cold", "shared-build-cold", "warm", "repeat-warm"}

type matrixGenerationConfig struct {
	MatrixReport, LocalCalibration, NetworkCalibration, ProcessCalibration, OutputDir string
}

type matrixFloorRun struct {
	Schema, RunID, MatrixSha256, Status     string
	Formal, FormalProtocolComplete          bool
	Repetitions, MatrixCases, SelectedCases int
	Samples                                 []matrixFloorSample `json:"samples"`
}

type matrixFloorSample struct {
	CommandPath, CaseID, StableCaseKey, Protocol, Status, RunID, MatrixSha256 string
	Args                                                                      []string `json:"args"`
	ExitCode                                                                  int      `json:"exitCode"`
	Repetition                                                                int      `json:"repetition"`
	WallMilliseconds, CPUMilliseconds                                         float64
	CacheDeltaBytes, OutputDeltaBytes                                         int64
	Stdout                                                                    string
	NetworkMetrics                                                            *matrixNetworkMetrics  `json:"networkMetrics"`
	ResourceMetrics                                                           *matrixResourceMetrics `json:"resourceMetrics"`
	Baseline                                                                  *matrixFloorBaseline   `json:"baseline"`
}

type matrixFloorBaseline struct {
	OutputMatch      *bool  `json:"outputMatch"`
	ComparisonStatus string `json:"comparisonStatus"`
	Unsupported      bool   `json:"unsupported"`
}

type matrixNetworkMetrics struct {
	Requests, FailedRequests, CanceledRequests               int
	ResponseBytes, UniquePayloadBytes, DuplicatePayloadBytes int64
}

type matrixResourceMetrics struct {
	Schema            string              `json:"schema"`
	PoolCapacity      map[string]int      `json:"poolCapacity"`
	ConnectionBudget  int                 `json:"connectionBudget"`
	StageDependencies map[string][]string `json:"stageDependencies"`
	Work              matrixResourceWork  `json:"work"`
}

type matrixResourceWork struct {
	Complete         bool                `json:"complete"`
	UncoveredClasses []string            `json:"uncoveredClasses"`
	Counters         []matrixWorkCounter `json:"counters"`
}

type matrixWorkCounter struct {
	Class, Unit string
	Units       uint64
}

type localFloorCalibration struct {
	Disk struct {
		SequentialWriteBytesPerSecond float64 `json:"sequentialWriteBytesPerSecond"`
		SequentialReadBytesPerSecond  float64 `json:"sequentialReadBytesPerSecond"`
	} `json:"disk"`
	Compute []matrixUnitCalibration `json:"compute"`
	Storage []matrixUnitCalibration `json:"storage"`
	Codecs  []matrixUnitCalibration `json:"codecs"`
}

type matrixUnitCalibration struct {
	Name, ResourceClass, WorkUnit  string
	BytesPerSecond, UnitsPerSecond float64
}

type networkFloorCalibration struct {
	Samples []struct {
		Name          string
		ResponseBytes int64 `json:"responseBytes"`
		BodyNanos     int64 `json:"bodyNanos"`
		TTFBNanos     int64 `json:"ttfbNanos"`
	} `json:"samples"`
}

type processFloorCalibration struct {
	P50Nanos int64 `json:"p50Nanos"`
}

type matrixFloorIndex struct {
	Schema       string                  `json:"schema"`
	RunID        string                  `json:"runId"`
	MatrixSha256 string                  `json:"matrixSha256"`
	MatrixReport string                  `json:"matrixReport"`
	Calibrations map[string]string       `json:"calibrations"`
	Summary      matrixFloorIndexSummary `json:"summary"`
	Cases        []matrixFloorIndexCase  `json:"cases"`
}

type matrixFloorIndexSummary struct {
	StableCaseProtocols int `json:"stableCaseProtocols"`
	Available           int `json:"available"`
	Missing             int `json:"missing"`
}

type matrixFloorIndexCase struct {
	CommandPath   string                 `json:"commandPath"`
	CaseID        string                 `json:"caseId"`
	StableCaseKey string                 `json:"stableCaseKey"`
	Protocol      string                 `json:"protocol"`
	Status        string                 `json:"status"`
	Reason        string                 `json:"reason,omitempty"`
	Input         string                 `json:"input,omitempty"`
	Output        string                 `json:"output,omitempty"`
	Samples       int                    `json:"samples"`
	Derivation    *matrixFloorDerivation `json:"derivation,omitempty"`
}

type matrixFloorDerivation struct {
	WorkCounters       []matrixWorkCounter     `json:"workCounters,omitempty"`
	WorkMeaning        string                  `json:"workMeaning"`
	NetworkUniqueBytes int64                   `json:"networkUniqueBytes"`
	DiskWriteBytes     int64                   `json:"diskWriteBytes"`
	OutputBytes        int64                   `json:"outputBytes"`
	NetworkStages      []matrixFloorStageAudit `json:"networkStages,omitempty"`
}

type matrixFloorStageAudit struct {
	Name                 string `json:"name"`
	ResourceClass        string `json:"resourceClass"`
	Requests             int    `json:"requests"`
	SuccessfulRequests   int    `json:"successfulRequests"`
	Waves                int    `json:"waves"`
	UniqueBytes          int64  `json:"uniqueBytes"`
	CriticalPathRTTNanos int64  `json:"criticalPathRttNanos"`
}

type observedStage struct {
	name               string
	requests, canceled int
	uniqueBytes        int64
}

type matrixFloorRates struct {
	processStart, metadataTTFB, largeTTFB int64
	metadataBody, largeBody               float64
	work                                  map[string]workCalibration
}

type workCalibration struct {
	class, unit, resource string
	rate                  float64
}

var detailedStagePattern = regexp.MustCompile(`^timing stage=(casc-[^ ]+) duration=[^ ]+ total=[^ ]+ requests=(\d+) uniqueBytes=(\d+) duplicateBytes=(\d+) canceled=(\d+)$`)

func generateMatrixFloors(cfg matrixGenerationConfig) error {
	if cfg.MatrixReport == "" || cfg.LocalCalibration == "" || cfg.NetworkCalibration == "" || cfg.ProcessCalibration == "" || cfg.OutputDir == "" {
		return errors.New("matrix mode requires -matrix-report, -local-calibration, -network-calibration, -process-calibration, and -output-dir")
	}
	var run matrixFloorRun
	if err := readJSON(cfg.MatrixReport, &run); err != nil {
		return fmt.Errorf("matrix report: %w", err)
	}
	if run.Schema != "wowdata.command-matrix-run.v1" {
		return fmt.Errorf("matrix report schema %q is not wowdata.command-matrix-run.v1", run.Schema)
	}
	if err := validateFloorMatrixRun(run); err != nil {
		return err
	}
	rates, err := loadMatrixFloorRates(cfg)
	if err != nil {
		return err
	}
	groups := make(map[string][]matrixFloorSample)
	for _, sample := range run.Samples {
		if sample.StableCaseKey == "" || sample.Protocol == "" {
			continue
		}
		key := sample.StableCaseKey + "\x00" + sample.Protocol
		groups[key] = append(groups[key], sample)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	index := matrixFloorIndex{
		Schema: matrixFloorIndexSchema, RunID: run.RunID, MatrixSha256: run.MatrixSha256, MatrixReport: filepath.ToSlash(cfg.MatrixReport),
		Calibrations: map[string]string{"local": filepath.ToSlash(cfg.LocalCalibration), "network": filepath.ToSlash(cfg.NetworkCalibration), "process": filepath.ToSlash(cfg.ProcessCalibration)},
		Cases:        []matrixFloorIndexCase{},
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return err
	}
	for _, key := range keys {
		samples := groups[key]
		entry := matrixFloorIndexCase{CommandPath: samples[0].CommandPath, CaseID: samples[0].CaseID, StableCaseKey: samples[0].StableCaseKey, Protocol: samples[0].Protocol, Samples: len(samples)}
		floorInput, derivation, reason := buildMatrixFloorInput(samples, rates)
		if reason != "" {
			entry.Status, entry.Reason = "missing", reason
			index.Summary.Missing++
		} else {
			name := floorFileName(entry)
			inputPath := filepath.Join(cfg.OutputDir, name+"-input.json")
			outputPath := filepath.Join(cfg.OutputDir, name+"-floor.json")
			result, calculateErr := calculate(floorInput)
			if calculateErr != nil {
				entry.Status, entry.Reason = "missing", "floor calculation: "+calculateErr.Error()
				index.Summary.Missing++
			} else if err := writeJSON(inputPath, floorInput); err != nil {
				return err
			} else {
				result.Schema = matrixCaseFloorSchema
				result.Derivation = derivation
				if err := writeJSON(outputPath, result); err != nil {
					return err
				}
				entry.Status, entry.Input, entry.Output, entry.Derivation = "available", filepath.ToSlash(inputPath), filepath.ToSlash(outputPath), derivation
				index.Summary.Available++
			}
		}
		index.Cases = append(index.Cases, entry)
	}
	index.Summary.StableCaseProtocols = len(index.Cases)
	return writeJSON(filepath.Join(cfg.OutputDir, "index.json"), index)
}

func validateFloorMatrixRun(run matrixFloorRun) error {
	if !run.Formal || !run.FormalProtocolComplete || run.Status != "pass" || run.RunID == "" || run.MatrixSha256 == "" {
		return fmt.Errorf("matrix report is not a passing identified formal run: formal=%t protocolComplete=%t status=%q runID=%q matrixSHA=%q", run.Formal, run.FormalProtocolComplete, run.Status, run.RunID, run.MatrixSha256)
	}
	if run.Repetitions < 10 || run.MatrixCases != formalMatrixCases || run.SelectedCases != run.MatrixCases {
		return fmt.Errorf("formal matrix completeness invalid: repetitions=%d matrixCases=%d selectedCases=%d", run.Repetitions, run.MatrixCases, run.SelectedCases)
	}
	groups := make(map[string]map[int]bool, formalMatrixCases*len(formalProtocols))
	cases := make(map[string]bool, formalMatrixCases)
	stableOwners := make(map[string]string, formalMatrixCases)
	for _, sample := range run.Samples {
		if sample.StableCaseKey == "" || sample.RunID != run.RunID || sample.MatrixSha256 != run.MatrixSha256 || !containsString(formalProtocols, sample.Protocol) || sample.Status != "pass" || sample.ExitCode != 0 || sample.Repetition < 1 || sample.Repetition > run.Repetitions {
			return fmt.Errorf("invalid formal floor sample %q/%q repetition=%d status=%q exit=%d", sample.StableCaseKey, sample.Protocol, sample.Repetition, sample.Status, sample.ExitCode)
		}
		if sample.Baseline == nil || !(sample.Baseline.OutputMatch != nil && *sample.Baseline.OutputMatch || sample.Baseline.Unsupported && sample.Baseline.ComparisonStatus == "unsupported") {
			return fmt.Errorf("formal floor sample baseline did not match for %q/%q repetition %d", sample.StableCaseKey, sample.Protocol, sample.Repetition)
		}
		if sample.NetworkMetrics != nil && (sample.NetworkMetrics.FailedRequests != 0 || sample.NetworkMetrics.DuplicatePayloadBytes != 0 || sample.NetworkMetrics.ResponseBytes > 0 && float64(sample.NetworkMetrics.UniquePayloadBytes)/float64(sample.NetworkMetrics.ResponseBytes) < .95) {
			return fmt.Errorf("formal floor sample network quality failed for %q/%q repetition %d", sample.StableCaseKey, sample.Protocol, sample.Repetition)
		}
		caseKey := sample.CommandPath + "\x00" + sample.StableCaseKey
		if owner, exists := stableOwners[sample.StableCaseKey]; exists && owner != sample.CommandPath {
			return fmt.Errorf("stable case key %q is shared by commands %q and %q", sample.StableCaseKey, owner, sample.CommandPath)
		}
		stableOwners[sample.StableCaseKey] = sample.CommandPath
		cases[caseKey] = true
		key := caseKey + "\x00" + sample.Protocol
		if groups[key] == nil {
			groups[key] = map[int]bool{}
		}
		if groups[key][sample.Repetition] {
			return fmt.Errorf("duplicate formal floor sample %q repetition %d", key, sample.Repetition)
		}
		groups[key][sample.Repetition] = true
	}
	if len(cases) != formalMatrixCases || len(groups) != formalMatrixCases*len(formalProtocols) {
		return fmt.Errorf("formal floor matrix has %d cases/%d case-protocol groups, want %d/%d", len(cases), len(groups), formalMatrixCases, formalMatrixCases*len(formalProtocols))
	}
	for key, repetitions := range groups {
		if len(repetitions) != run.Repetitions {
			return fmt.Errorf("formal floor group %q has %d repetitions, want %d", key, len(repetitions), run.Repetitions)
		}
	}
	return nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func loadMatrixFloorRates(cfg matrixGenerationConfig) (matrixFloorRates, error) {
	var local localFloorCalibration
	var network networkFloorCalibration
	var process processFloorCalibration
	if err := readJSON(cfg.LocalCalibration, &local); err != nil {
		return matrixFloorRates{}, fmt.Errorf("local calibration: %w", err)
	}
	if err := readJSON(cfg.NetworkCalibration, &network); err != nil {
		return matrixFloorRates{}, fmt.Errorf("network calibration: %w", err)
	}
	if err := readJSON(cfg.ProcessCalibration, &process); err != nil {
		return matrixFloorRates{}, fmt.Errorf("process calibration: %w", err)
	}
	rates := matrixFloorRates{processStart: process.P50Nanos, work: map[string]workCalibration{}}
	calibrations := append([]matrixUnitCalibration(nil), local.Compute...)
	calibrations = append(calibrations, local.Storage...)
	calibrations = append(calibrations, local.Codecs...)
	for _, value := range calibrations {
		class := strings.TrimSpace(value.ResourceClass)
		unit := strings.TrimSpace(value.WorkUnit)
		rate := value.UnitsPerSecond
		if class == "" || unit == "" || rate <= 0 {
			continue
		}
		resource := "cpu"
		if class == "file-read" || class == "file-write" {
			resource = "disk"
		}
		rates.work[class+"\x00"+unit] = workCalibration{class: class, unit: unit, resource: resource, rate: rate}
	}
	if local.Disk.SequentialReadBytesPerSecond > 0 {
		if _, exists := rates.work["file-read\x00bytes"]; !exists {
			rates.work["file-read\x00bytes"] = workCalibration{class: "file-read", unit: "bytes", resource: "disk", rate: local.Disk.SequentialReadBytesPerSecond}
		}
	}
	if local.Disk.SequentialWriteBytesPerSecond > 0 {
		if _, exists := rates.work["file-write\x00bytes"]; !exists {
			rates.work["file-write\x00bytes"] = workCalibration{class: "file-write", unit: "bytes", resource: "disk", rate: local.Disk.SequentialWriteBytesPerSecond}
		}
	}
	rates.metadataBody, rates.metadataTTFB = medianNetworkProbe(network.Samples, "cdn-small-24")
	rates.largeBody, rates.largeTTFB = medianNetworkProbe(network.Samples, "cdn-multi")
	if rates.processStart <= 0 || rates.metadataBody <= 0 || rates.largeBody <= 0 || rates.metadataTTFB <= 0 || rates.largeTTFB <= 0 {
		return matrixFloorRates{}, errors.New("calibration reports lack positive process, cdn-small-24, or cdn-multi measurements")
	}
	return rates, nil
}

func medianNetworkProbe(samples []struct {
	Name          string
	ResponseBytes int64 `json:"responseBytes"`
	BodyNanos     int64 `json:"bodyNanos"`
	TTFBNanos     int64 `json:"ttfbNanos"`
}, name string) (float64, int64) {
	var body []float64
	var ttfb []int64
	for _, sample := range samples {
		if sample.Name == name && sample.ResponseBytes > 0 && sample.BodyNanos > 0 && sample.TTFBNanos > 0 {
			body = append(body, float64(sample.ResponseBytes)/(float64(sample.BodyNanos)/1e9))
			ttfb = append(ttfb, sample.TTFBNanos)
		}
	}
	return medianFloat(body), medianInt64(ttfb)
}

func buildMatrixFloorInput(samples []matrixFloorSample, rates matrixFloorRates) (input, *matrixFloorDerivation, string) {
	for _, sample := range samples {
		if sample.Status != "pass" || sample.ExitCode != 0 {
			return input{}, nil, "one or more matrix samples did not pass"
		}
	}
	walls := make([]float64, 0, len(samples))
	for _, sample := range samples {
		walls = append(walls, sample.WallMilliseconds)
	}
	medianSample := samples[medianSampleIndex(samples)]
	var outputObservations []int64
	for _, sample := range samples {
		stdoutBytes, err := referencedFileSize(sample.Stdout)
		if err != nil {
			return input{}, nil, "stdout evidence: " + err.Error()
		}
		outputObservations = append(outputObservations, stdoutBytes+maxInt64(0, sample.OutputDeltaBytes))
	}
	outputBytes := medianInt64(outputObservations)
	counters, counterReason := exactWorkCounters(samples, rates)
	if counterReason != "" {
		return input{}, nil, counterReason
	}
	derivation := &matrixFloorDerivation{WorkCounters: counters, WorkMeaning: "instrumented format-specific units matched exactly to resourceClass/workUnit calibration; process CPU time is observation only", OutputBytes: outputBytes}
	cal := calibration{ProcessStartNanos: rates.processStart, ResourceClasses: map[string]resourceClassCalibration{
		"cdn-metadata":    {Resource: "network", BytesPerSecond: rates.metadataBody},
		"cdn-large-range": {Resource: "network", BytesPerSecond: rates.largeBody},
		"cdn-shared":      {Resource: "network", BytesPerSecond: rates.largeBody},
	}}
	result := input{Command: samples[0].CommandPath + " [" + samples[0].CaseID + "]", Protocol: samples[0].Protocol, CaseID: samples[0].CaseID, StableCaseKey: samples[0].StableCaseKey, RunID: samples[0].RunID, MatrixSha256: samples[0].MatrixSha256, Calibration: cal,
		Actual: actual{P50Nanos: millisecondsToNanos(percentileFloat(walls, .50)), P95Nanos: millisecondsToNanos(percentileFloat(walls, .95)), MaxNanos: millisecondsToNanos(maxFloat(walls))}}
	lastDependencies := make([]string, 0, len(counters)+8)
	for _, counter := range counters {
		key := counter.Class + "\x00" + counter.Unit
		matched := rates.work[key]
		cal.ResourceClasses[matched.class] = resourceClassCalibration{Resource: matched.resource, BytesPerSecond: matched.rate}
		id := sanitizeFloorName("work-" + counter.Class + "-" + counter.Unit)
		current := node{ID: id}
		if counter.Units > math.MaxInt64 {
			return input{}, nil, "work counter overflows int64 for " + counter.Class
		}
		units := int64(counter.Units)
		if matched.resource == "disk" {
			current.RequiredDiskBytes, current.DiskResourceClass = units, matched.class
		} else {
			current.RequiredCPUWorkBytes, current.CPUResourceClass = units, matched.class
		}
		result.Nodes = append(result.Nodes, current)
		lastDependencies = append(lastDependencies, id)
	}
	result.Calibration = cal
	stages, hasNetwork, parseErr := aggregateDetailedStages(samples)
	if parseErr != nil {
		return input{}, nil, parseErr.Error()
	}
	if hasNetwork {
		dependencies, dependencyReason := exactStageDependencies(samples, stages)
		if dependencyReason != "" {
			return input{}, nil, dependencyReason
		}
		var stageBytes int64
		for _, stage := range stages {
			stageBytes += stage.uniqueBytes
			class, capacity, ttfb := classifyFloorStage(stage.name, medianSample.ResourceMetrics, rates)
			if capacity <= 0 {
				return input{}, nil, "resource pool capacity is absent for " + stage.name
			}
			successful := stage.requests - stage.canceled
			if successful < 1 && stage.uniqueBytes > 0 {
				successful = 1
			}
			waves := (successful + capacity - 1) / capacity
			id := sanitizeFloorName(stage.name)
			current := node{ID: id, CriticalPathRTTNanos: int64(waves) * ttfb, RequiredUniqueNetworkBytes: stage.uniqueBytes, NetworkResourceClass: class, SharedNetworkResourceClass: "cdn-shared"}
			for _, dependency := range dependencies[stage.name] {
				current.DependsOn = append(current.DependsOn, sanitizeFloorName(dependency))
			}
			result.Nodes = append(result.Nodes, current)
			lastDependencies = append(lastDependencies, id)
			derivation.NetworkStages = append(derivation.NetworkStages, matrixFloorStageAudit{Name: stage.name, ResourceClass: class, Requests: stage.requests, SuccessfulRequests: successful, Waves: waves, UniqueBytes: stage.uniqueBytes, CriticalPathRTTNanos: current.CriticalPathRTTNanos})
		}
		derivation.NetworkUniqueBytes = stageBytes
	}
	result.Nodes = append(result.Nodes, node{ID: "resource-join", DependsOn: lastDependencies})
	return result, derivation, ""
}

func exactWorkCounters(samples []matrixFloorSample, rates matrixFloorRates) ([]matrixWorkCounter, string) {
	var canonical string
	var result []matrixWorkCounter
	for _, sample := range samples {
		metrics := sample.ResourceMetrics
		if metrics == nil || metrics.Schema != "wowdata.resource-metrics.v2" || !metrics.Work.Complete || len(metrics.Work.UncoveredClasses) != 0 {
			return nil, "resource metrics v2 work coverage is absent or incomplete"
		}
		merged := map[string]matrixWorkCounter{}
		for _, counter := range metrics.Work.Counters {
			if counter.Class == "" || counter.Unit == "" {
				return nil, "resource work counter class/unit is absent"
			}
			key := counter.Class + "\x00" + counter.Unit
			value := merged[key]
			value.Class, value.Unit = counter.Class, counter.Unit
			if math.MaxUint64-value.Units < counter.Units {
				return nil, "resource work counter overflow for " + counter.Class
			}
			value.Units += counter.Units
			merged[key] = value
		}
		keys := make([]string, 0, len(merged))
		for key := range merged {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		current := make([]matrixWorkCounter, 0, len(keys))
		for _, key := range keys {
			counter := merged[key]
			if _, ok := rates.work[key]; !ok {
				return nil, "exact calibration is absent for " + counter.Class + "/" + counter.Unit
			}
			current = append(current, counter)
		}
		classes := map[string]bool{}
		for _, counter := range current {
			classes[counter.Class] = true
		}
		for _, required := range requiredOutputClasses(sample) {
			if !classes[required] {
				return nil, "output work counter is absent for " + required
			}
		}
		encoded, _ := json.Marshal(current)
		if canonical == "" {
			canonical, result = string(encoded), current
		} else if canonical != string(encoded) {
			return nil, "resource work counters differ across repetitions"
		}
	}
	if len(result) == 0 {
		return nil, "resource metrics v2 has no work counters"
	}
	return result, ""
}

func requiredOutputClasses(sample matrixFloorSample) []string {
	required := []string{"json-encode"}
	var output string
	for index, argument := range sample.Args {
		if argument == "--output" && index+1 < len(sample.Args) {
			output = sample.Args[index+1]
		}
		if strings.HasPrefix(argument, "--output=") {
			output = strings.TrimPrefix(argument, "--output=")
		}
	}
	if output == "" {
		return required
	}
	required = append(required, "file-write")
	extension := strings.ToLower(filepath.Ext(output))
	if extension == ".png" || sample.CommandPath == "icon export" {
		required = append(required, "png-encode")
	}
	if extension == ".webp" {
		required = append(required, "webp-encode")
	}
	return required
}

func exactStageDependencies(samples []matrixFloorSample, stages []observedStage) (map[string][]string, string) {
	known := map[string]bool{}
	for _, stage := range stages {
		known[stage.name] = true
	}
	var canonical string
	var result map[string][]string
	for _, sample := range samples {
		if sample.ResourceMetrics == nil {
			return nil, "resource metrics are absent for network DAG"
		}
		copyMap := map[string][]string{}
		for _, stage := range stages {
			dependencies, exists := sample.ResourceMetrics.StageDependencies[stage.name]
			if !exists {
				return nil, "explicit network stage dependencies are absent for " + stage.name
			}
			copyMap[stage.name] = append([]string(nil), dependencies...)
			sort.Strings(copyMap[stage.name])
			for _, dependency := range dependencies {
				if !known[dependency] {
					return nil, "network stage dependency refers to unknown stage " + dependency
				}
			}
		}
		encoded, _ := json.Marshal(copyMap)
		if canonical == "" {
			canonical, result = string(encoded), copyMap
		} else if canonical != string(encoded) {
			return nil, "network stage dependencies differ across repetitions"
		}
	}
	return result, ""
}

func aggregateDetailedStages(samples []matrixFloorSample) ([]observedStage, bool, error) {
	withNetwork := 0
	var all [][]observedStage
	for _, sample := range samples {
		if sample.NetworkMetrics == nil || sample.NetworkMetrics.UniquePayloadBytes == 0 {
			continue
		}
		withNetwork++
		stages, err := readDetailedStages(sample)
		if err != nil {
			return nil, false, err
		}
		var attributed int64
		for _, stage := range stages {
			attributed += stage.uniqueBytes
		}
		if attributed != sample.NetworkMetrics.UniquePayloadBytes {
			return nil, false, fmt.Errorf("network stage attribution is incomplete: stages=%d totalUnique=%d", attributed, sample.NetworkMetrics.UniquePayloadBytes)
		}
		if len(all) > 0 {
			if len(stages) != len(all[0]) {
				return nil, false, errors.New("network stage sequence differs across repetitions")
			}
			for index := range stages {
				if stages[index].name != all[0][index].name {
					return nil, false, errors.New("network stage sequence differs across repetitions")
				}
			}
		}
		all = append(all, stages)
	}
	if withNetwork == 0 {
		return nil, false, nil
	}
	if withNetwork != len(samples) {
		return nil, false, errors.New("network presence differs across repetitions")
	}
	result := make([]observedStage, len(all[0]))
	for index := range result {
		var requests, canceled []int64
		var unique []int64
		for _, stages := range all {
			requests = append(requests, int64(stages[index].requests))
			canceled = append(canceled, int64(stages[index].canceled))
			unique = append(unique, stages[index].uniqueBytes)
		}
		result[index] = observedStage{name: all[0][index].name, requests: int(medianInt64(requests)), canceled: int(medianInt64(canceled)), uniqueBytes: medianInt64(unique)}
	}
	return result, true, nil
}

func readDetailedStages(sample matrixFloorSample) ([]observedStage, error) {
	if sample.Stdout == "" {
		return nil, errors.New("stdout path is absent")
	}
	// stderr and stdout are siblings in formal matrix evidence.
	data, err := os.ReadFile(filepath.Join(filepath.Dir(sample.Stdout), "stderr.log"))
	if err != nil {
		return nil, fmt.Errorf("detailed timing stderr is unavailable: %w", err)
	}
	var stages []observedStage
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		match := detailedStagePattern.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		requests, _ := strconv.Atoi(match[2])
		bytes, _ := strconv.ParseInt(match[3], 10, 64)
		canceled, _ := strconv.Atoi(match[5])
		stages = append(stages, observedStage{name: match[1], requests: requests, canceled: canceled, uniqueBytes: bytes})
	}
	if len(stages) == 0 {
		return nil, errors.New("detailed casc stage metrics are absent")
	}
	return stages, nil
}

func classifyFloorStage(name string, resources *matrixResourceMetrics, rates matrixFloorRates) (string, int, int64) {
	large := strings.Contains(name, "root") || strings.Contains(name, "file") || strings.Contains(name, "payload")
	pool := "metadata"
	if large {
		pool = "large-range"
	}
	capacity := 0
	if resources != nil {
		capacity = resources.PoolCapacity[pool]
		if resources.ConnectionBudget > 0 && (capacity == 0 || resources.ConnectionBudget < capacity) {
			capacity = resources.ConnectionBudget
		}
	}
	if large {
		return "cdn-large-range", capacity, rates.largeTTFB
	}
	return "cdn-metadata", capacity, rates.metadataTTFB
}

func medianSampleIndex(samples []matrixFloorSample) int {
	indices := make([]int, len(samples))
	for i := range indices {
		indices[i] = i
	}
	sort.Slice(indices, func(i, j int) bool {
		return samples[indices[i]].WallMilliseconds < samples[indices[j]].WallMilliseconds
	})
	return indices[(len(indices)-1)/2]
}

func referencedFileSize(path string) (int64, error) {
	if path == "" {
		return 0, errors.New("path is absent")
	}
	info, err := os.Stat(filepath.FromSlash(path))
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func floorFileName(value matrixFloorIndexCase) string {
	return sanitizeFloorName(value.StableCaseKey + "-" + value.Protocol)
}

func sanitizeFloorName(value string) string {
	var output strings.Builder
	dash := false
	for _, r := range strings.ToLower(value) {
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

func readJSON(path string, output any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, output)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func medianFloat(values []float64) float64 { return percentileFloat(values, .50) }
func medianInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]int64(nil), values...)
	sort.Slice(copyValues, func(i, j int) bool { return copyValues[i] < copyValues[j] })
	return copyValues[(len(copyValues)-1)/2]
}
func percentileFloat(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]float64(nil), values...)
	sort.Float64s(copyValues)
	index := int(math.Ceil(percentile*float64(len(copyValues)))) - 1
	if index < 0 {
		index = 0
	}
	return copyValues[index]
}
func maxFloat(values []float64) float64 {
	var maximum float64
	for _, value := range values {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}
func millisecondsToNanos(value float64) int64 { return int64(math.Round(value * 1e6)) }
func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
