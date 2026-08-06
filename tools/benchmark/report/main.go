// Command report assembles wowdata performance evidence without inventing missing results.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"wowdata/internal/app"

	"github.com/spf13/cobra"
)

const reportSchema = "wowdata.final-performance-report.v1"
const expectedFormalMatrixCases = 91

var requiredProtocols = []string{"independent-cold", "shared-build-cold", "warm", "repeat-warm"}

type config struct {
	EvidenceRoot, OutputDir, LocalCalibration, NetworkCalibration             string
	Corpus, Resume, MetadataTournament, LargeRangeTournament, ChunkTournament string
	Regression, Matrix, FloorIndex                                            string
}
type finalReport struct {
	Schema            string          `json:"schema"`
	GeneratedAt       string          `json:"generatedAt"`
	Status            string          `json:"status"`
	Summary           summary         `json:"summary"`
	Evidence          []evidence      `json:"evidence"`
	Global            globalEvidence  `json:"global"`
	Commands          []commandReport `json:"commands"`
	AboveFloor        []aboveFloor    `json:"above1_5xFloor"`
	UnclassifiedFloor []string        `json:"unclassifiedFloor"`
}
type summary struct {
	LeafCommands                  int `json:"leafCommands"`
	CommandsWithMeasurements      int `json:"commandsWithMeasurements"`
	CommandsWithFloors            int `json:"commandsWithFloors"`
	CommandsWithRegressionPass    int `json:"commandsWithRegressionPass"`
	CommandsWithRegressionFailure int `json:"commandsWithRegressionFailure"`
	CommandsMissingEvidence       int `json:"commandsMissingEvidence"`
}
type evidence struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Bytes  int64  `json:"bytes,omitempty"`
}
type globalEvidence struct {
	LocalCalibration     statusValue `json:"localCalibration"`
	NetworkCalibration   statusValue `json:"networkCalibration"`
	DB2Corpus            statusValue `json:"db2Corpus"`
	Resume               statusValue `json:"resume"`
	MetadataTournament   statusValue `json:"metadataTournament"`
	LargeRangeTournament statusValue `json:"largeRangeTournament"`
	ChunkTournament      statusValue `json:"chunkTournament"`
	Regression           statusValue `json:"regression"`
}
type statusValue struct {
	Status string         `json:"status"`
	Reason string         `json:"reason,omitempty"`
	Values map[string]any `json:"values,omitempty"`
}
type commandReport struct {
	Path          string                     `json:"path"`
	Status        string                     `json:"status"`
	CaseID        string                     `json:"caseId,omitempty"`
	StableCaseKey string                     `json:"stableCaseKey,omitempty"`
	RunID         string                     `json:"runId,omitempty"`
	Revision      string                     `json:"revision,omitempty"`
	BinarySHA256  string                     `json:"binarySha256,omitempty"`
	Baseline      metricStatus               `json:"baseline"`
	Optimized     metricStatus               `json:"optimized"`
	Floor         floorStatus                `json:"tFloor"`
	Efficiency    efficiencyStatus           `json:"efficiency"`
	Memory        metricStatus               `json:"memory"`
	Requests      metricStatus               `json:"requests"`
	Regression    regressionStatus           `json:"regression"`
	CriticalPath  criticalPathStatus         `json:"criticalPath"`
	Protocols     map[string]protocolMetrics `json:"protocols,omitempty"`
	Evidence      []string                   `json:"evidence,omitempty"`
	Missing       []string                   `json:"missing,omitempty"`
	Cases         []caseReport               `json:"cases,omitempty"`
}
type caseReport struct {
	CaseID        string                     `json:"caseId"`
	StableCaseKey string                     `json:"stableCaseKey"`
	RunID         string                     `json:"runId"`
	Revision      string                     `json:"revision,omitempty"`
	BinarySHA256  string                     `json:"binarySha256"`
	Status        string                     `json:"status"`
	Baseline      metricStatus               `json:"baseline"`
	Optimized     metricStatus               `json:"optimized"`
	Memory        metricStatus               `json:"memory"`
	Requests      metricStatus               `json:"requests"`
	Regression    regressionStatus           `json:"regression"`
	Protocols     map[string]protocolMetrics `json:"protocols"`
	Evidence      []string                   `json:"evidence,omitempty"`
}
type metricStatus struct {
	Status  string  `json:"status"`
	Reason  string  `json:"reason,omitempty"`
	P50     float64 `json:"p50,omitempty"`
	P95     float64 `json:"p95,omitempty"`
	Max     float64 `json:"max,omitempty"`
	Unit    string  `json:"unit,omitempty"`
	Samples int     `json:"samples,omitempty"`
}
type floorStatus struct {
	Status       string  `json:"status"`
	Reason       string  `json:"reason,omitempty"`
	Milliseconds float64 `json:"milliseconds,omitempty"`
	Dominant     string  `json:"dominant,omitempty"`
	Source       string  `json:"source,omitempty"`
	SelectedBy   string  `json:"selectedBy,omitempty"`
}
type efficiencyStatus struct {
	Status   string  `json:"status"`
	Reason   string  `json:"reason,omitempty"`
	P50Ratio float64 `json:"p50Ratio,omitempty"`
	P95Ratio float64 `json:"p95Ratio,omitempty"`
}
type regressionStatus struct {
	Status          string   `json:"status"`
	Reason          string   `json:"reason,omitempty"`
	SampleStatuses  []string `json:"sampleStatuses,omitempty"`
	Golden          string   `json:"golden"`
	BaselineOutput  string   `json:"baselineOutput"`
	CrossProtocol   string   `json:"crossProtocol"`
	WithinProtocol  string   `json:"withinProtocol"`
	RepeatWarmCache string   `json:"repeatWarmCache"`
}
type criticalPathStatus struct {
	Status string   `json:"status"`
	Reason string   `json:"reason,omitempty"`
	Nodes  []string `json:"nodes,omitempty"`
	Nanos  int64    `json:"nanos,omitempty"`
}
type protocolMetrics struct {
	Optimized    metricStatus       `json:"optimized"`
	Baseline     metricStatus       `json:"baseline"`
	PeakMemory   metricStatus       `json:"peakMemory"`
	Requests     metricStatus       `json:"requests"`
	Floor        floorStatus        `json:"tFloor"`
	Efficiency   efficiencyStatus   `json:"efficiency"`
	CriticalPath criticalPathStatus `json:"criticalPath"`
}
type aboveFloor struct {
	Command       string   `json:"command"`
	CaseID        string   `json:"caseId"`
	StableCaseKey string   `json:"stableCaseKey"`
	Protocol      string   `json:"protocol"`
	P50Ratio      float64  `json:"p50Ratio"`
	P95Ratio      float64  `json:"p95Ratio"`
	CriticalPath  []string `json:"criticalPath"`
}

type matrixRun struct {
	Schema, GeneratedAt, RunID, Revision, Binary, BinarySha256, BaselineBinary, BaselineBinarySha256, MatrixSha256 string
	Repetitions                                                                                                    int
	Formal, FormalProtocolComplete                                                                                 bool
	MatrixCases, SelectedCases                                                                                     int
	Status                                                                                                         string
	Samples                                                                                                        []matrixSample `json:"samples"`
	sourcePath                                                                                                     string
	sourceModified                                                                                                 time.Time
}
type matrixSample struct {
	CommandPath, CaseID, StableCaseKey, Protocol, Target, Status string
	RunID, Revision, BinarySha256, MatrixSha256                  string
	Repetition                                                   int
	WallMilliseconds, PeakWorkingSetBytes                        float64
	NetworkMetrics                                               *networkMetrics `json:"networkMetrics"`
	Baseline                                                     *baselineSample `json:"baseline"`
	GoldenMatch                                                  *bool           `json:"goldenMatch"`
	CrossProtocolMatch                                           *bool           `json:"crossProtocolMatch"`
	OutputComparisonPolicy                                       string          `json:"outputComparisonPolicy"`
	OutputComparisonMatch                                        *bool           `json:"outputComparisonMatch"`
	OutputComparisonReferenceAvailable                           *bool           `json:"outputComparisonReferenceAvailable"`
	RepeatWarmCacheStable                                        *bool           `json:"repeatWarmCacheStable"`
}
type networkMetrics struct {
	Requests, FailedRequests, CanceledRequests               int
	ResponseBytes, UniquePayloadBytes, DuplicatePayloadBytes int64
}
type baselineSample struct {
	WallMilliseconds, PeakWorkingSetBytes float64
	OutputMatch                           *bool  `json:"outputMatch"`
	ComparisonStatus                      string `json:"comparisonStatus"`
	Unsupported                           bool   `json:"unsupported"`
}
type floorReport struct {
	Schema, Command, Protocol, CaseID, StableCaseKey, RunID, MatrixSha256 string
	FloorMilliseconds                                                     float64
	CriticalPathNanos                                                     int64
	CriticalPath                                                          []string
	AggregateResourceFloor                                                struct{ Dominant string }
	Actual                                                                struct{ P50FloorRatio, P95FloorRatio float64 }
	sourcePath                                                            string
	sourceModified                                                        time.Time
	explicit                                                              bool
	missingReason                                                         string
}

type floorIndex struct {
	Schema, RunID, MatrixSha256, MatrixReport string
	Summary                                   struct{ StableCaseProtocols, Available, Missing int }
	Cases                                     []floorIndexCase
}

type floorIndexCase struct {
	CommandPath, CaseID, StableCaseKey, Protocol, Status, Reason, Output string
	Samples                                                              int
}
type corpusReport struct {
	Schema                              string
	CacheDeltaBytes, PeakHeapAllocBytes int64
	Targets                             []struct {
		BuildName                                     string
		Loaded, Missing, Errors, DifferentialFailures int
	}
}
type resumeReport struct {
	Schema, Status string
	Samples        []struct {
		ResumedExitCode int
		FinalSha256     string
	}
}
type tournamentReport struct {
	Schema  string                `json:"schema"`
	Summary []tournamentCandidate `json:"summary"`
}
type tournamentCandidate struct {
	Candidate      int     `json:"candidate"`
	SuccessfulRuns int     `json:"successfulRuns"`
	FailedAttempts int     `json:"failedAttempts"`
	P50WallMs      float64 `json:"p50WallMs"`
	P95WallMs      float64 `json:"p95WallMs"`
}
type regressionFinal struct {
	Schema             string `json:"schema"`
	GeneratedAt        string `json:"generatedAt"`
	Status             string `json:"status"`
	Published          bool   `json:"published"`
	PerformanceFinding string `json:"performanceFinding"`
	BaseChecks         []struct {
		Name     string `json:"name"`
		ExitCode int    `json:"exitCode"`
	} `json:"baseChecks"`
	PostfixGoChecks []struct {
		Name     string `json:"name"`
		ExitCode int    `json:"exitCode"`
	} `json:"postfixGoChecks"`
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.EvidenceRoot, "evidence-root", "analyze/benchmark", "evidence root")
	flag.StringVar(&cfg.OutputDir, "output", "analyze/benchmark/final-report-draft", "output directory")
	flag.StringVar(&cfg.LocalCalibration, "local-calibration", "analyze/benchmark/calibration/local.json", "local calibration")
	flag.StringVar(&cfg.NetworkCalibration, "network-calibration", "analyze/benchmark/network-calibration/report.json", "network calibration")
	flag.StringVar(&cfg.Corpus, "corpus", "analyze/benchmark/db2-corpus-r18-v2i-full/report.json", "DB2 corpus report")
	flag.StringVar(&cfg.Resume, "resume", "analyze/benchmark/resume-kill-build-r4-final/report.json", "resume report")
	flag.StringVar(&cfg.MetadataTournament, "metadata-tournament", "analyze/benchmark/metadata-tournament-r12-10x-rotating-tail64/report.json", "metadata tournament")
	flag.StringVar(&cfg.LargeRangeTournament, "large-range-tournament", "analyze/benchmark/large-range-tournament-r36-demand-final/report.json", "on-demand large-range tournament")
	flag.StringVar(&cfg.ChunkTournament, "chunk-tournament", "analyze/benchmark/chunk-tournament-r13-10x-rotating-tail64-meta24/report.json", "chunk tournament")
	flag.StringVar(&cfg.Regression, "regression", "analyze/benchmark/regression-r18/final-summary.json", "full regression summary")
	flag.StringVar(&cfg.Matrix, "matrix-report", "", "formal command matrix report (required)")
	flag.StringVar(&cfg.FloorIndex, "floor-index", "", "case/protocol floor index (required)")
	flag.Parse()
	r, err := generate(cfg, collectLeaves(app.NewRootCommand()))
	if err != nil {
		fatal(err)
	}
	if err = writeReports(cfg.OutputDir, r); err != nil {
		fatal(err)
	}
}

func generate(cfg config, paths []string) (finalReport, error) {
	r := finalReport{Schema: reportSchema, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	r.AboveFloor = []aboveFloor{}
	r.UnclassifiedFloor = []string{}
	r.Commands = []commandReport{}
	r.Evidence = []evidence{}
	sort.Strings(paths)
	r.Summary.LeafCommands = len(paths)
	r.Global.LocalCalibration = genericStatus(selectEvidence(cfg.LocalCalibration, cfg.EvidenceRoot, "", &r.Evidence), "wowdata local calibration", &r.Evidence)
	r.Global.NetworkCalibration = genericStatus(selectEvidence(cfg.NetworkCalibration, cfg.EvidenceRoot, "wowdata.network-calibration.v1", &r.Evidence), "wowdata network calibration", &r.Evidence)
	r.Global.DB2Corpus = corpusStatus(selectEvidence(cfg.Corpus, cfg.EvidenceRoot, "wowdata.db2-corpus.v2", &r.Evidence), &r.Evidence)
	r.Global.Resume = resumeStatus(selectEvidence(cfg.Resume, cfg.EvidenceRoot, "wowdata.resume-kill-benchmark.v1", &r.Evidence), &r.Evidence)
	r.Global.MetadataTournament = tournamentStatus(selectEvidence(cfg.MetadataTournament, cfg.EvidenceRoot, "wowdata.network-tournament.v1", &r.Evidence), &r.Evidence)
	r.Global.LargeRangeTournament = tournamentStatus(selectEvidence(cfg.LargeRangeTournament, cfg.EvidenceRoot, "wowdata.network-tournament.v1", &r.Evidence), &r.Evidence)
	r.Global.ChunkTournament = tournamentStatus(selectEvidence(cfg.ChunkTournament, cfg.EvidenceRoot, "wowdata.network-tournament.v1", &r.Evidence), &r.Evidence)
	r.Global.Regression = fullRegressionStatus(selectEvidence(cfg.Regression, cfg.EvidenceRoot, "wowdata.regression-final.v1", &r.Evidence), &r.Evidence)
	run, floors, err := loadFormalEvidence(cfg.Matrix, cfg.FloorIndex, paths)
	if err != nil {
		return r, err
	}
	r.Evidence = append(r.Evidence, fileEvidence("formal-command-matrix", cfg.Matrix), fileEvidence("case-floor-index", cfg.FloorIndex))
	latest := latestSamples([]matrixRun{run})
	for _, path := range paths {
		c := buildCommand(path, latest[path], floors)
		r.Commands = append(r.Commands, c)
		measuredCases := 0
		for _, fixedInput := range c.Cases {
			if fixedInput.Optimized.Status == "available" {
				measuredCases++
			}
		}
		if len(c.Cases) > 0 && measuredCases == len(c.Cases) {
			r.Summary.CommandsWithMeasurements++
		}
		allFloors := len(c.Cases) > 0
		for _, fixedInput := range c.Cases {
			for _, protocol := range requiredProtocols {
				allFloors = allFloors && fixedInput.Protocols[protocol].Floor.Status == "available"
			}
		}
		if allFloors {
			r.Summary.CommandsWithFloors++
		} else {
			r.UnclassifiedFloor = append(r.UnclassifiedFloor, path)
		}
		switch c.Regression.Status {
		case "pass":
			r.Summary.CommandsWithRegressionPass++
		case "fail":
			r.Summary.CommandsWithRegressionFailure++
		}
		if c.Status == "missing" {
			r.Summary.CommandsMissingEvidence++
		}
		for _, fixedInput := range c.Cases {
			for _, protocol := range requiredProtocols {
				metrics := fixedInput.Protocols[protocol]
				if metrics.Efficiency.Status == "available" && (metrics.Efficiency.P50Ratio > 1.5 || metrics.Efficiency.P95Ratio > 1.5) {
					r.AboveFloor = append(r.AboveFloor, aboveFloor{Command: path, CaseID: fixedInput.CaseID, StableCaseKey: fixedInput.StableCaseKey, Protocol: protocol, P50Ratio: metrics.Efficiency.P50Ratio, P95Ratio: metrics.Efficiency.P95Ratio, CriticalPath: metrics.CriticalPath.Nodes})
				}
			}
		}
	}
	sort.Slice(r.Evidence, func(i, j int) bool {
		if r.Evidence[i].Kind == r.Evidence[j].Kind {
			return r.Evidence[i].Path < r.Evidence[j].Path
		}
		return r.Evidence[i].Kind < r.Evidence[j].Kind
	})
	r.Status = "draft"
	if r.Summary.CommandsMissingEvidence == 0 && len(r.UnclassifiedFloor) == 0 && r.Summary.CommandsWithRegressionFailure == 0 && allGlobalEvidencePassed(r.Global) {
		r.Status = "complete"
	}
	return r, nil
}

func scanEvidence(root string) ([]matrixRun, []floorReport, []evidence, error) {
	var runs []matrixRun
	var floors []floorReport
	var found []evidence
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var head struct {
			Schema string `json:"schema"`
		}
		if json.Unmarshal(data, &head) != nil {
			return nil
		}
		switch head.Schema {
		case "wowdata.command-matrix-run.v1":
			var v matrixRun
			if json.Unmarshal(data, &v) == nil {
				v.sourcePath = path
				if info, statErr := d.Info(); statErr == nil {
					v.sourceModified = info.ModTime()
				}
				runs = append(runs, v)
				found = append(found, fileEvidence("command-matrix-run", path))
			}
		case "wowdata.command-floor.v1":
			var v floorReport
			if json.Unmarshal(data, &v) == nil {
				v.sourcePath = path
				if info, statErr := d.Info(); statErr == nil {
					v.sourceModified = info.ModTime()
				}
				floors = append(floors, v)
				found = append(found, fileEvidence("command-floor", path))
			}
		}
		return nil
	})
	return runs, floors, found, err
}

type selectedSample struct {
	run    matrixRun
	sample matrixSample
}

func latestSamples(runs []matrixRun) map[string][][]selectedSample {
	type group struct {
		identity string
		run      matrixRun
		caseID   string
		caseKey  string
		samples  []selectedSample
	}
	groups := map[string]*group{}
	for _, run := range runs {
		for _, s := range run.Samples {
			caseKey := s.StableCaseKey
			if caseKey == "" {
				caseKey = s.CaseID
			}
			binary := firstNonEmpty(s.BinarySha256, run.BinarySha256, run.Binary)
			revision := firstNonEmpty(s.Revision, run.Revision)
			runID := firstNonEmpty(s.RunID, run.RunID, run.sourcePath)
			identity := strings.Join([]string{s.CommandPath, caseKey, binary, revision, runID}, "\x00")
			g := groups[identity]
			if g == nil {
				g = &group{identity: identity, run: run, caseID: s.CaseID, caseKey: caseKey}
				groups[identity] = g
			}
			g.samples = append(g.samples, selectedSample{run, s})
		}
	}
	chosen := map[string]*group{}
	for _, candidate := range groups {
		path := candidate.samples[0].sample.CommandPath
		key := path + "\x00" + candidate.caseKey
		current := chosen[key]
		if current == nil || newerRun(candidate.run, current.run) {
			chosen[key] = candidate
		}
	}
	out := map[string][][]selectedSample{}
	for _, g := range chosen {
		path := g.samples[0].sample.CommandPath
		out[path] = append(out[path], g.samples)
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool {
			return stableKeyFor(out[k][i][0]) < stableKeyFor(out[k][j][0])
		})
		for _, samples := range out[k] {
			sort.Slice(samples, func(i, j int) bool {
				a, b := samples[i].sample, samples[j].sample
				if a.Protocol == b.Protocol {
					return a.Repetition < b.Repetition
				}
				return a.Protocol < b.Protocol
			})
		}
	}
	return out
}

func stableKeyFor(value selectedSample) string {
	return firstNonEmpty(value.sample.StableCaseKey, value.sample.CaseID)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func newerRun(candidate, current matrixRun) bool {
	candidateTime, candidateErr := time.Parse(time.RFC3339Nano, candidate.GeneratedAt)
	currentTime, currentErr := time.Parse(time.RFC3339Nano, current.GeneratedAt)
	if candidateErr == nil && currentErr == nil && !candidateTime.Equal(currentTime) {
		return candidateTime.After(currentTime)
	}
	if candidate.GeneratedAt != current.GeneratedAt {
		return candidate.GeneratedAt > current.GeneratedAt
	}
	return candidate.sourceModified.After(current.sourceModified)
}
func latestFloors(values []floorReport, paths []string) map[string]floorReport {
	out := map[string]floorReport{}
	for _, v := range values {
		path := commandPathForFloor(v.Command, paths)
		if path == "" {
			continue
		}
		old, exists := out[path]
		if !exists || v.explicit && !old.explicit || v.explicit == old.explicit && v.sourceModified.After(old.sourceModified) {
			out[path] = v
		}
	}
	return out
}
func commandPathForFloor(command string, paths []string) string {
	command = strings.TrimSpace(command)
	best := ""
	for _, path := range paths {
		if (command == path || strings.HasPrefix(command, path+" ")) && len(path) > len(best) {
			best = path
		}
	}
	return best
}
func readFloor(path string) (floorReport, bool) {
	var value floorReport
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &value) != nil || value.Schema != "wowdata.command-floor.v1" {
		return floorReport{}, false
	}
	value.sourcePath = path
	if info, statErr := os.Stat(path); statErr == nil {
		value.sourceModified = info.ModTime()
	}
	return value, true
}

func floorJoinKey(stableCaseKey, protocol string) string {
	return stableCaseKey + "\x00" + protocol
}

func loadFormalEvidence(matrixPath, indexPath string, paths []string) (matrixRun, map[string]floorReport, error) {
	if strings.TrimSpace(matrixPath) == "" || strings.TrimSpace(indexPath) == "" {
		return matrixRun{}, nil, errors.New("-matrix-report and -floor-index are required")
	}
	var run matrixRun
	if !readJSON(matrixPath, &run) || run.Schema != "wowdata.command-matrix-run.v1" {
		return matrixRun{}, nil, errors.New("formal matrix report is absent, invalid, or has the wrong schema")
	}
	if err := validateFormalMatrix(run, paths); err != nil {
		return matrixRun{}, nil, err
	}
	var index floorIndex
	if !readJSON(indexPath, &index) || index.Schema != "wowdata.command-floor-index.v1" {
		return matrixRun{}, nil, errors.New("floor index is absent, invalid, or has the wrong schema")
	}
	if index.RunID == "" || index.RunID != run.RunID || index.MatrixSha256 == "" || index.MatrixSha256 != run.MatrixSha256 {
		return matrixRun{}, nil, fmt.Errorf("floor index identity does not match formal matrix: runID %q/%q matrixSHA %q/%q", index.RunID, run.RunID, index.MatrixSha256, run.MatrixSha256)
	}
	expected := expectedFormalMatrixCases * len(requiredProtocols)
	if index.Summary.StableCaseProtocols != expected || len(index.Cases) != expected || index.Summary.Available+index.Summary.Missing != expected {
		return matrixRun{}, nil, fmt.Errorf("floor index completeness is %d entries (%d available + %d missing), want %d", len(index.Cases), index.Summary.Available, index.Summary.Missing, expected)
	}
	result := make(map[string]floorReport, expected)
	for _, entry := range index.Cases {
		key := floorJoinKey(entry.StableCaseKey, entry.Protocol)
		if entry.StableCaseKey == "" || !contains(requiredProtocols, entry.Protocol) || entry.Samples < 10 {
			return matrixRun{}, nil, fmt.Errorf("invalid floor index entry %q/%q: samples=%d", entry.StableCaseKey, entry.Protocol, entry.Samples)
		}
		if _, duplicate := result[key]; duplicate {
			return matrixRun{}, nil, fmt.Errorf("duplicate floor index entry for %q/%q", entry.StableCaseKey, entry.Protocol)
		}
		if entry.Status == "missing" {
			if strings.TrimSpace(entry.Reason) == "" {
				return matrixRun{}, nil, fmt.Errorf("missing floor %q/%q has no reason", entry.StableCaseKey, entry.Protocol)
			}
			result[key] = floorReport{StableCaseKey: entry.StableCaseKey, Protocol: entry.Protocol, CaseID: entry.CaseID, missingReason: entry.Reason}
			continue
		}
		if entry.Status != "available" || strings.TrimSpace(entry.Output) == "" {
			return matrixRun{}, nil, fmt.Errorf("invalid floor status %q for %q/%q", entry.Status, entry.StableCaseKey, entry.Protocol)
		}
		var value floorReport
		if !readJSON(entry.Output, &value) || value.Schema != "wowdata.command-case-floor.v1" {
			return matrixRun{}, nil, fmt.Errorf("case floor %q is absent, invalid, or has the wrong schema", entry.Output)
		}
		if value.StableCaseKey != entry.StableCaseKey || value.Protocol != entry.Protocol || value.CaseID != entry.CaseID || value.RunID != run.RunID || value.MatrixSha256 != run.MatrixSha256 {
			return matrixRun{}, nil, fmt.Errorf("case floor identity differs from index for %q/%q", entry.StableCaseKey, entry.Protocol)
		}
		value.sourcePath = entry.Output
		result[key] = value
	}
	return run, result, nil
}

func validateFormalMatrix(run matrixRun, paths []string) error {
	if !run.Formal || !run.FormalProtocolComplete || run.Status != "pass" {
		return fmt.Errorf("matrix report is not a passing formal four-protocol run: formal=%t protocolComplete=%t status=%q", run.Formal, run.FormalProtocolComplete, run.Status)
	}
	if run.RunID == "" || run.MatrixSha256 == "" || run.BinarySha256 == "" || run.BaselineBinarySha256 == "" || run.Repetitions < 10 || run.MatrixCases != expectedFormalMatrixCases || run.SelectedCases != run.MatrixCases || samePath(run.Binary, run.BaselineBinary) {
		return fmt.Errorf("formal matrix identity/completeness invalid: runID=%q matrixSHA=%q repetitions=%d matrixCases=%d selectedCases=%d", run.RunID, run.MatrixSha256, run.Repetitions, run.MatrixCases, run.SelectedCases)
	}
	known := make(map[string]bool, len(paths))
	for _, path := range paths {
		known[path] = true
	}
	type observed struct {
		command, caseID string
		repetitions     map[int]bool
	}
	groups := make(map[string]*observed, expectedFormalMatrixCases*len(requiredProtocols))
	cases := make(map[string]bool, expectedFormalMatrixCases)
	commands := make(map[string]bool, len(paths))
	stableOwners := make(map[string]string, expectedFormalMatrixCases)
	for _, sample := range run.Samples {
		if !known[sample.CommandPath] || sample.StableCaseKey == "" || sample.RunID != run.RunID || sample.MatrixSha256 != run.MatrixSha256 || sample.BinarySha256 != run.BinarySha256 || !contains(requiredProtocols, sample.Protocol) || sample.Status != "pass" {
			return fmt.Errorf("invalid formal sample command=%q case=%q protocol=%q status=%q", sample.CommandPath, sample.StableCaseKey, sample.Protocol, sample.Status)
		}
		if sample.Baseline == nil || !(sample.Baseline.OutputMatch != nil && *sample.Baseline.OutputMatch || sample.Baseline.Unsupported && sample.Baseline.ComparisonStatus == "unsupported") {
			return fmt.Errorf("baseline comparison did not pass for %q/%q/%q repetition %d", sample.CommandPath, sample.StableCaseKey, sample.Protocol, sample.Repetition)
		}
		if sample.NetworkMetrics != nil && (sample.NetworkMetrics.FailedRequests != 0 || sample.NetworkMetrics.DuplicatePayloadBytes != 0 || sample.NetworkMetrics.ResponseBytes > 0 && float64(sample.NetworkMetrics.UniquePayloadBytes)/float64(sample.NetworkMetrics.ResponseBytes) < .95) {
			return fmt.Errorf("network quality failed for %q/%q/%q repetition %d", sample.CommandPath, sample.StableCaseKey, sample.Protocol, sample.Repetition)
		}
		caseKey := sample.CommandPath + "\x00" + sample.StableCaseKey
		if owner, exists := stableOwners[sample.StableCaseKey]; exists && owner != sample.CommandPath {
			return fmt.Errorf("stable case key %q is shared by commands %q and %q", sample.StableCaseKey, owner, sample.CommandPath)
		}
		stableOwners[sample.StableCaseKey] = sample.CommandPath
		commands[sample.CommandPath] = true
		cases[caseKey] = true
		key := floorJoinKey(caseKey, sample.Protocol)
		group := groups[key]
		if group == nil {
			group = &observed{sample.CommandPath, sample.CaseID, map[int]bool{}}
			groups[key] = group
		}
		if group.command != sample.CommandPath || group.caseID != sample.CaseID || sample.Repetition < 1 || sample.Repetition > run.Repetitions || group.repetitions[sample.Repetition] {
			return fmt.Errorf("duplicate or inconsistent formal sample for %q/%q/%q repetition %d", sample.CommandPath, sample.StableCaseKey, sample.Protocol, sample.Repetition)
		}
		group.repetitions[sample.Repetition] = true
	}
	if len(cases) != expectedFormalMatrixCases || len(groups) != expectedFormalMatrixCases*len(requiredProtocols) {
		return fmt.Errorf("formal matrix contains %d cases/%d case-protocol groups, want %d/%d", len(cases), len(groups), expectedFormalMatrixCases, expectedFormalMatrixCases*len(requiredProtocols))
	}
	if len(commands) != len(paths) {
		return fmt.Errorf("formal matrix covers %d leaf commands, want %d", len(commands), len(paths))
	}
	for key, group := range groups {
		if len(group.repetitions) != run.Repetitions {
			return fmt.Errorf("formal group %q has %d repetitions, want %d", key, len(group.repetitions), run.Repetitions)
		}
	}
	return nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func buildCommand(path string, groups [][]selectedSample, floors map[string]floorReport) commandReport {
	c := commandReport{Path: path, Protocols: map[string]protocolMetrics{}}
	if len(groups) == 0 {
		c.Status = "missing"
		reason := "no command-matrix-run sample found"
		c.Baseline = missingMetric(reason)
		c.Optimized = missingMetric(reason)
		c.Memory = missingMetric(reason)
		c.Requests = missingMetric(reason)
		c.Regression = missingRegression(reason)
	} else {
		c.Status = "measured"
		for _, samples := range groups {
			value := buildCase(samples, floors)
			c.Cases = append(c.Cases, value)
			for _, hash := range value.Evidence {
				c.Evidence = appendUnique(c.Evidence, hash)
			}
			if value.Status != "measured" {
				c.Status = "partial"
			}
		}
		c.Regression = aggregateRegressions(c.Cases)
		if len(c.Cases) == 1 {
			value := c.Cases[0]
			c.CaseID, c.StableCaseKey, c.RunID, c.Revision, c.BinarySHA256 = value.CaseID, value.StableCaseKey, value.RunID, value.Revision, value.BinarySHA256
			c.Baseline, c.Optimized, c.Memory, c.Requests, c.Protocols = value.Baseline, value.Optimized, value.Memory, value.Requests, value.Protocols
		} else {
			reason := "multiple fixed inputs; use cases[] for unmixed metrics"
			c.Baseline, c.Optimized, c.Memory, c.Requests = missingMetric(reason), missingMetric(reason), missingMetric(reason), missingMetric(reason)
		}
	}
	if len(c.Cases) != 1 {
		reason := "command has zero or multiple fixed inputs; use cases[].protocols for case/protocol floors"
		c.Floor = floorStatus{Status: "missing", Reason: reason}
		c.Efficiency = efficiencyStatus{Status: "missing", Reason: reason}
		c.CriticalPath = criticalPathStatus{Status: "missing", Reason: reason}
	} else if metrics, ok := c.Cases[0].Protocols["independent-cold"]; !ok {
		reason := "independent-cold protocol is absent"
		c.Floor = floorStatus{Status: "missing", Reason: reason}
		c.Efficiency = efficiencyStatus{Status: "missing", Reason: reason}
		c.CriticalPath = criticalPathStatus{Status: "missing", Reason: reason}
	} else {
		c.Floor, c.Efficiency, c.CriticalPath = metrics.Floor, metrics.Efficiency, metrics.CriticalPath
	}
	for _, name := range []struct{ n, s string }{{"baseline", c.Baseline.Status}, {"optimized", c.Optimized.Status}, {"T_floor", c.Floor.Status}, {"efficiency", c.Efficiency.Status}, {"memory", c.Memory.Status}, {"requests", c.Requests.Status}, {"regression", c.Regression.Status}, {"critical path", c.CriticalPath.Status}} {
		if name.s == "missing" {
			c.Missing = append(c.Missing, name.n)
		}
	}
	return c
}

func buildCase(samples []selectedSample, floors map[string]floorReport) caseReport {
	first := samples[0]
	value := caseReport{
		CaseID: first.sample.CaseID, StableCaseKey: stableKeyFor(first),
		RunID:    firstNonEmpty(first.sample.RunID, first.run.RunID, first.run.sourcePath),
		Revision: firstNonEmpty(first.sample.Revision, first.run.Revision), BinarySHA256: firstNonEmpty(first.sample.BinarySha256, first.run.BinarySha256),
		Status: "partial", Protocols: map[string]protocolMetrics{},
	}
	byProtocol := map[string][]selectedSample{}
	for _, sample := range samples {
		byProtocol[sample.sample.Protocol] = append(byProtocol[sample.sample.Protocol], sample)
		value.Evidence = appendUnique(value.Evidence, firstNonEmpty(sample.sample.BinarySha256, sample.run.BinarySha256))
	}
	protocolNames := make([]string, 0, len(byProtocol))
	for protocol, samples := range byProtocol {
		metrics := metricsFor(samples)
		floor := floors[floorJoinKey(value.StableCaseKey, protocol)]
		if floor.Schema == "" {
			reason := floor.missingReason
			if reason == "" {
				reason = "case/protocol floor index entry is absent"
			}
			metrics.Floor = floorStatus{Status: "missing", Reason: reason}
			metrics.Efficiency = efficiencyStatus{Status: "missing", Reason: reason}
			metrics.CriticalPath = criticalPathStatus{Status: "missing", Reason: reason}
		} else {
			metrics.Floor = floorStatus{Status: "available", Milliseconds: floor.FloorMilliseconds, Dominant: floor.AggregateResourceFloor.Dominant, Source: filepath.ToSlash(floor.sourcePath), SelectedBy: "formal floor index identity join"}
			metrics.CriticalPath = criticalPathStatus{Status: "available", Nodes: floor.CriticalPath, Nanos: floor.CriticalPathNanos}
			if floor.FloorMilliseconds > 0 && metrics.Optimized.Status == "available" {
				metrics.Efficiency = efficiencyStatus{Status: "available", P50Ratio: metrics.Optimized.P50 / floor.FloorMilliseconds, P95Ratio: metrics.Optimized.P95 / floor.FloorMilliseconds}
			} else {
				metrics.Efficiency = efficiencyStatus{Status: "missing", Reason: "floor or optimized p50/p95 is unavailable"}
			}
		}
		value.Protocols[protocol] = metrics
		protocolNames = append(protocolNames, protocol)
	}
	sort.Strings(protocolNames)
	primary := byProtocol["independent-cold"]
	if len(primary) == 0 && len(protocolNames) > 0 {
		primary = byProtocol[protocolNames[0]]
	}
	metrics := metricsFor(primary)
	value.Optimized, value.Baseline, value.Memory, value.Requests = metrics.Optimized, metrics.Baseline, metrics.PeakMemory, metrics.Requests
	value.Regression = regressionFor(samples)
	if value.Regression.Status == "pass" {
		value.Status = "measured"
	}
	return value
}

func aggregateRegressions(values []caseReport) regressionStatus {
	result := regressionStatus{Status: "pass", Golden: "pass", BaselineOutput: "pass", CrossProtocol: "pass", RepeatWarmCache: "pass"}
	for _, value := range values {
		result.SampleStatuses = append(result.SampleStatuses, value.Regression.SampleStatuses...)
		for destination, status := range map[*string]string{&result.Golden: value.Regression.Golden, &result.BaselineOutput: value.Regression.BaselineOutput, &result.CrossProtocol: value.Regression.CrossProtocol, &result.RepeatWarmCache: value.Regression.RepeatWarmCache} {
			if status == "fail" || status == "missing" && *destination != "fail" {
				*destination = status
			}
		}
		if value.Regression.Status == "fail" {
			result.Status, result.Reason = "fail", "one or more fixed-input cases failed"
		} else if value.Regression.Status != "pass" && result.Status == "pass" {
			result.Status, result.Reason = "missing", "one or more fixed-input cases lack complete regression evidence"
		}
	}
	return result
}

func metricsFor(values []selectedSample) protocolMetrics {
	var wall, base, memory, baseMemory, requests []float64
	for _, v := range values {
		s := v.sample
		wall = append(wall, s.WallMilliseconds)
		memory = append(memory, s.PeakWorkingSetBytes)
		if s.NetworkMetrics != nil {
			requests = append(requests, float64(s.NetworkMetrics.Requests))
		}
		if s.Baseline != nil && v.run.BaselineBinary != "" && !samePath(v.run.BaselineBinary, v.run.Binary) {
			base = append(base, s.Baseline.WallMilliseconds)
			baseMemory = append(baseMemory, s.Baseline.PeakWorkingSetBytes)
		}
	}
	return protocolMetrics{Optimized: metric(wall, "ms"), Baseline: metricOrMissing(base, "ms", "baseline binary sample absent"), PeakMemory: metric(memory, "bytes"), Requests: metricOrMissing(requests, "requests", "network metrics absent")}
}
func metric(v []float64, unit string) metricStatus {
	sort.Float64s(v)
	return metricStatus{Status: "available", P50: pct(v, .5), P95: pct(v, .95), Max: pct(v, 1), Unit: unit, Samples: len(v)}
}
func metricOrMissing(v []float64, unit, reason string) metricStatus {
	if len(v) == 0 {
		return missingMetric(reason)
	}
	return metric(v, unit)
}
func missingMetric(reason string) metricStatus {
	return metricStatus{Status: "missing", Reason: reason}
}
func regressionFor(values []selectedSample) regressionStatus {
	r := regressionStatus{Status: "pass", Golden: "missing", BaselineOutput: "missing", CrossProtocol: "missing", WithinProtocol: "missing", RepeatWarmCache: "missing"}
	goldSeen, baseSeen, outputSeen, warmSeen := false, false, false, false
	for _, v := range values {
		s := v.sample
		r.SampleStatuses = append(r.SampleStatuses, s.Status)
		if s.Status != "pass" {
			r.Status = "fail"
			r.Reason = "one or more matrix samples failed"
		}
		if s.GoldenMatch != nil {
			goldSeen = true
			r.Golden = boolStatus(*s.GoldenMatch)
			if !*s.GoldenMatch {
				r.Status = "fail"
			}
		}
		if s.Baseline != nil && s.Baseline.OutputMatch != nil && v.run.BaselineBinary != "" && !samePath(v.run.BaselineBinary, v.run.Binary) {
			baseSeen = true
			r.BaselineOutput = boolStatus(*s.Baseline.OutputMatch)
			if !*s.Baseline.OutputMatch {
				r.Status = "fail"
			}
		}
		if s.OutputComparisonMatch != nil {
			if s.OutputComparisonPolicy == "stable-within-protocol" {
				if s.OutputComparisonReferenceAvailable != nil && *s.OutputComparisonReferenceAvailable {
					outputSeen = true
					r.WithinProtocol = boolStatus(*s.OutputComparisonMatch)
				}
			} else {
				outputSeen = true
				r.CrossProtocol = boolStatus(*s.OutputComparisonMatch)
			}
			if !*s.OutputComparisonMatch {
				r.Status = "fail"
				r.Reason = "output mismatch under the command's declared comparison policy"
			}
		} else if s.CrossProtocolMatch != nil {
			// Backward-compatible evidence from runs predating explicit policies.
			outputSeen = true
			r.CrossProtocol = boolStatus(*s.CrossProtocolMatch)
			if !*s.CrossProtocolMatch {
				r.Status = "fail"
			}
		}
		if s.RepeatWarmCacheStable != nil {
			warmSeen = true
			r.RepeatWarmCache = boolStatus(*s.RepeatWarmCacheStable)
			if !*s.RepeatWarmCacheStable {
				r.Status = "fail"
			}
		}
	}
	if r.Status != "fail" && (!goldSeen || !baseSeen || !outputSeen || !warmSeen) {
		r.Status = "missing"
		r.Reason = "golden, baseline output, declared output comparison, or repeat-warm evidence is incomplete"
	}
	return r
}
func missingRegression(reason string) regressionStatus {
	return regressionStatus{Status: "missing", Reason: reason, Golden: "missing", BaselineOutput: "missing", CrossProtocol: "missing", WithinProtocol: "missing", RepeatWarmCache: "missing"}
}

func genericStatus(path, kind string, e *[]evidence) statusValue {
	data, err := os.ReadFile(path)
	if err != nil {
		return statusValue{Status: "missing", Reason: err.Error()}
	}
	var v map[string]any
	if json.Unmarshal(data, &v) != nil {
		return statusValue{Status: "fail", Reason: "invalid JSON"}
	}
	*e = append(*e, fileEvidence(kind, path))
	schemaValue := v["schema"]
	if schemaValue == nil {
		schemaValue = v["schemaVersion"]
	}
	return statusValue{Status: "available", Values: map[string]any{"schema": schemaValue, "generatedAt": v["generatedAt"]}}
}
func selectEvidence(explicit, root, wantedSchema string, audit *[]evidence) string {
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			*audit = append(*audit, evidence{Kind: "selection-audit", Path: filepath.ToSlash(explicit), Status: "available", Reason: "selected explicit path; pass/fail status did not influence selection"})
			return explicit
		}
	}
	if wantedSchema == "" {
		return explicit
	}
	type candidate struct {
		path                string
		generated, modified time.Time
	}
	var best candidate
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		var head struct{ Schema, GeneratedAt string }
		if json.Unmarshal(data, &head) != nil || head.Schema != wantedSchema {
			return nil
		}
		var modified time.Time
		if info, statErr := d.Info(); statErr == nil {
			modified = info.ModTime()
		}
		generated, _ := time.Parse(time.RFC3339Nano, head.GeneratedAt)
		if best.path == "" || generated.After(best.generated) || generated.Equal(best.generated) && modified.After(best.modified) {
			best = candidate{path, generated, modified}
		}
		return nil
	})
	if best.path != "" {
		*audit = append(*audit, evidence{Kind: "selection-audit", Path: filepath.ToSlash(best.path), Status: "available", Reason: "explicit path missing; selected latest generatedAt then mtime; pass/fail status did not influence selection"})
		return best.path
	}
	return explicit
}
func corpusStatus(path string, e *[]evidence) statusValue {
	var v corpusReport
	if !readJSON(path, &v) {
		return statusValue{Status: "missing", Reason: "corpus report absent or invalid"}
	}
	*e = append(*e, fileEvidence("db2-corpus", path))
	failures, errs, loaded := 0, 0, 0
	for _, t := range v.Targets {
		failures += t.DifferentialFailures
		errs += t.Errors
		loaded += t.Loaded
	}
	status := "pass"
	reason := ""
	if failures > 0 || errs > 0 {
		status = "fail"
		reason = "corpus contains differential failures or load errors"
	}
	return statusValue{Status: status, Reason: reason, Values: map[string]any{"loaded": loaded, "differentialFailures": failures, "errors": errs, "peakHeapAllocBytes": v.PeakHeapAllocBytes, "cacheDeltaBytes": v.CacheDeltaBytes}}
}
func resumeStatus(path string, e *[]evidence) statusValue {
	var v resumeReport
	if !readJSON(path, &v) {
		return statusValue{Status: "missing", Reason: "resume report absent or invalid"}
	}
	*e = append(*e, fileEvidence("resume", path))
	status := v.Status
	for _, s := range v.Samples {
		if s.ResumedExitCode != 0 || s.FinalSha256 == "" {
			status = "fail"
		}
	}
	return statusValue{Status: status, Values: map[string]any{"samples": len(v.Samples)}}
}
func tournamentStatus(path string, e *[]evidence) statusValue {
	var v tournamentReport
	if !readJSON(path, &v) {
		return statusValue{Status: "missing", Reason: "tournament report absent or invalid"}
	}
	*e = append(*e, fileEvidence("network-tournament", path))
	return statusValue{Status: "available", Values: map[string]any{"candidates": v.Summary}}
}
func fullRegressionStatus(path string, e *[]evidence) statusValue {
	var value regressionFinal
	if !readJSON(path, &value) || value.Schema != "wowdata.regression-final.v1" {
		return statusValue{Status: "missing", Reason: "full regression summary absent or invalid"}
	}
	*e = append(*e, fileEvidence("regression", path))
	failed := []string{}
	for _, check := range value.BaseChecks {
		if check.ExitCode != 0 {
			failed = append(failed, check.Name)
		}
	}
	for _, check := range value.PostfixGoChecks {
		if check.ExitCode != 0 {
			failed = append(failed, check.Name)
		}
	}
	status := value.Status
	if len(failed) > 0 {
		status = "fail"
	}
	return statusValue{Status: status, Values: map[string]any{"generatedAt": value.GeneratedAt, "baseChecks": len(value.BaseChecks), "postfixGoChecks": len(value.PostfixGoChecks), "failedChecks": failed, "published": value.Published, "performanceFinding": value.PerformanceFinding}}
}
func readJSON(path string, v any) bool {
	data, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(data, v) == nil
}
func fileEvidence(kind, path string) evidence {
	info, err := os.Stat(path)
	if err != nil {
		return evidence{Kind: kind, Path: filepath.ToSlash(path), Status: "missing", Reason: err.Error()}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return evidence{Kind: kind, Path: filepath.ToSlash(path), Status: "missing", Reason: err.Error()}
	}
	sum := sha256.Sum256(data)
	return evidence{Kind: kind, Path: filepath.ToSlash(path), Status: "available", Bytes: info.Size(), SHA256: hex.EncodeToString(sum[:])}
}

func writeReports(dir string, r finalReport) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "report.json"), append(data, '\n'), 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "report.md"), []byte(markdown(r)), 0644)
}
func markdown(r finalReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# wowdata End-to-End Performance Report (Draft)\n\nGenerated: `%s`  \nStatus: **%s**\n\n", r.GeneratedAt, r.Status)
	fmt.Fprintf(&b, "Commands: %d; measured: %d; floors: %d; regression failures: %d; missing command evidence: %d.\n\n", r.Summary.LeafCommands, r.Summary.CommandsWithMeasurements, r.Summary.CommandsWithFloors, r.Summary.CommandsWithRegressionFailure, r.Summary.CommandsMissingEvidence)
	b.WriteString("## Commands\n\n| Command | Fixed input / run | Protocol | Baseline p50 | Optimized p50 | T_floor | Efficiency p50/p95 | Peak memory | Requests | Regression | Critical path |\n|---|---|---|---:|---:|---:|---:|---:|---:|---|---|\n")
	for _, c := range r.Commands {
		if len(c.Cases) == 0 {
			fmt.Fprintf(&b, "| `%s` | missing | missing | missing | missing | missing | missing | missing | missing | %s | missing |\n", c.Path, statusCell(c.Regression.Status, c.Regression.Reason))
			continue
		}
		for _, fixedInput := range c.Cases {
			identity := fmt.Sprintf("`%s` / `%s`", shortKey(fixedInput.StableCaseKey), fixedInput.RunID)
			for _, protocol := range requiredProtocols {
				metrics := fixedInput.Protocols[protocol]
				fmt.Fprintf(&b, "| `%s` | %s | `%s` | %s | %s | %s | %s | %s | %s | %s | %s |\n", c.Path, identity, protocol, metricCell(metrics.Baseline), metricCell(metrics.Optimized), floorCell(metrics.Floor), efficiencyCell(metrics.Efficiency), metricCell(metrics.PeakMemory), metricCell(metrics.Requests), statusCell(fixedInput.Regression.Status, fixedInput.Regression.Reason), criticalCell(metrics.CriticalPath))
			}
		}
	}
	b.WriteString("\n## Above 1.5x Floor\n\n")
	if len(r.AboveFloor) == 0 {
		b.WriteString("No classified command exceeds 1.5x. This is not a pass while floor evidence is missing.\n")
	} else {
		for _, v := range r.AboveFloor {
			fmt.Fprintf(&b, "- `%s` [`%s`, `%s`]: p50 %.3fx, p95 %.3fx; `%s`\n", v.Command, shortKey(v.StableCaseKey), v.Protocol, v.P50Ratio, v.P95Ratio, strings.Join(v.CriticalPath, " -> "))
		}
	}
	b.WriteString("\n## Missing Floor Classification\n\n")
	for _, v := range r.UnclassifiedFloor {
		fmt.Fprintf(&b, "- `%s`: missing command floor evidence\n", v)
	}
	b.WriteString("\n## Global Evidence\n\n")
	for _, v := range []struct {
		name string
		s    statusValue
	}{{"Local calibration", r.Global.LocalCalibration}, {"Network calibration", r.Global.NetworkCalibration}, {"DB2 corpus", r.Global.DB2Corpus}, {"Resume", r.Global.Resume}, {"Metadata tournament", r.Global.MetadataTournament}, {"Large-range tournament", r.Global.LargeRangeTournament}, {"Chunk tournament", r.Global.ChunkTournament}, {"Full regression", r.Global.Regression}} {
		fmt.Fprintf(&b, "- %s: **%s**", v.name, v.s.Status)
		if v.s.Reason != "" {
			fmt.Fprintf(&b, " - %s", v.s.Reason)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func allGlobalEvidencePassed(value globalEvidence) bool {
	for _, status := range []string{
		value.LocalCalibration.Status, value.NetworkCalibration.Status, value.DB2Corpus.Status,
		value.Resume.Status, value.MetadataTournament.Status, value.LargeRangeTournament.Status, value.ChunkTournament.Status, value.Regression.Status,
	} {
		if status != "pass" && status != "available" {
			return false
		}
	}
	return true
}

func collectLeaves(root *cobra.Command) []string {
	var out []string
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		children := c.Commands()
		if len(children) == 0 {
			out = append(out, strings.TrimPrefix(c.CommandPath(), "wowdata "))
			return
		}
		for _, child := range children {
			walk(child)
		}
	}
	walk(root)
	sort.Strings(out)
	return out
}
func pct(v []float64, q float64) float64 {
	if len(v) == 0 {
		return 0
	}
	return v[int(float64(len(v)-1)*q+.5)]
}
func boolStatus(v bool) string {
	if v {
		return "pass"
	}
	return "fail"
}
func appendUnique(v []string, s string) []string {
	for _, x := range v {
		if x == s {
			return v
		}
	}
	return append(v, s)
}
func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr == nil && rightErr == nil {
		return strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
	}
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}
func metricCell(v metricStatus) string {
	if v.Status != "available" {
		return statusCell(v.Status, v.Reason)
	}
	return fmt.Sprintf("%.2f %s", v.P50, v.Unit)
}
func floorCell(v floorStatus) string {
	if v.Status != "available" {
		return statusCell(v.Status, v.Reason)
	}
	return fmt.Sprintf("%.2f ms", v.Milliseconds)
}
func efficiencyCell(v efficiencyStatus) string {
	if v.Status != "available" {
		return statusCell(v.Status, v.Reason)
	}
	return fmt.Sprintf("%.3fx / %.3fx", v.P50Ratio, v.P95Ratio)
}
func caseEfficiencyCell(value caseReport, floor floorStatus) string {
	if floor.Status != "available" || floor.Milliseconds <= 0 || value.Optimized.Status != "available" {
		return "missing"
	}
	return fmt.Sprintf("%.3fx / %.3fx", value.Optimized.P50/floor.Milliseconds, value.Optimized.P95/floor.Milliseconds)
}
func criticalCell(v criticalPathStatus) string {
	if v.Status != "available" {
		return statusCell(v.Status, v.Reason)
	}
	return strings.Join(v.Nodes, " -> ")
}
func statusCell(status, reason string) string {
	if reason == "" {
		return status
	}
	return status + ": " + strings.ReplaceAll(reason, "|", "/")
}
func identityCell(value commandReport) string {
	if value.StableCaseKey == "" {
		return "missing"
	}
	return fmt.Sprintf("`%s` / `%s`", shortKey(value.StableCaseKey), value.RunID)
}
func shortKey(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}
func fatal(err error) {
	if err == nil {
		err = errors.New("unknown error")
	}
	fmt.Fprintln(os.Stderr, "report:", err)
	os.Exit(1)
}
