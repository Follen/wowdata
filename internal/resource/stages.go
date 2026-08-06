package resource

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type StageID uint64

const (
	StageWaveInitial            = "initial"
	StageWaveResourceSecondWave = "resource-second-wave"

	StageRelationHard     = "hard"
	StageRelationJoin     = "join"
	StageRelationFallback = "fallback"
)

type StageSpan struct {
	ID                 StageID `json:"id"`
	ParentID           StageID `json:"parentId,omitempty"`
	Name               string  `json:"name"`
	Wave               string  `json:"wave"`
	Instance           string  `json:"instance,omitempty"`
	Condition          string  `json:"condition,omitempty"`
	StartOffsetNanos   int64   `json:"startOffsetNanos"`
	ServiceOffsetNanos int64   `json:"serviceOffsetNanos,omitempty"`
	EndOffsetNanos     int64   `json:"endOffsetNanos"`
	DurationNanos      int64   `json:"durationNanos"`
	Status             string  `json:"status"`
	ErrorClass         string  `json:"errorClass,omitempty"`
}

type StageDependency struct {
	StageID     StageID `json:"stageId"`
	DependsOnID StageID `json:"dependsOnId"`
	Relation    string  `json:"relation"`
}

type StageWork struct {
	StageID                 StageID       `json:"stageId"`
	NetworkUniqueBytes      uint64        `json:"networkUniqueBytes,omitempty"`
	NetworkTransferredBytes uint64        `json:"networkTransferredBytes,omitempty"`
	DiskReadBytes           uint64        `json:"diskReadBytes,omitempty"`
	DiskWriteBytes          uint64        `json:"diskWriteBytes,omitempty"`
	OutputBytes             uint64        `json:"outputBytes,omitempty"`
	CPU                     []WorkCounter `json:"cpu,omitempty"`
}

type StageSnapshot struct {
	Complete         bool              `json:"complete"`
	UnattributedWork bool              `json:"unattributedWork,omitempty"`
	Spans            []StageSpan       `json:"spans"`
	Dependencies     []StageDependency `json:"dependencies"`
	Work             []StageWork       `json:"work"`
}

type StageOptions struct {
	ParentID  StageID
	Wave      string
	Instance  string
	Condition string
	DependsOn []StageDependency
}

type stageContextKey struct{}

type stageRecorder struct {
	mu               sync.Mutex
	epoch            time.Time
	nextID           atomic.Uint64
	spans            map[StageID]*StageSpan
	dependencies     []StageDependency
	work             map[StageID]*StageWork
	unattributedWork bool
}

var stages atomic.Pointer[stageRecorder]

func init() { ResetStages() }

func ResetStages() {
	r := &stageRecorder{epoch: time.Now(), spans: make(map[StageID]*StageSpan), work: make(map[StageID]*StageWork)}
	stages.Store(r)
}

func StartStage(name string, opts StageOptions) StageID {
	r := stageMeter()
	id := StageID(r.nextID.Add(1))
	wave := opts.Wave
	if wave == "" {
		wave = StageWaveInitial
	}
	now := time.Since(r.epoch).Nanoseconds()
	r.mu.Lock()
	r.spans[id] = &StageSpan{ID: id, ParentID: opts.ParentID, Name: name, Wave: wave, Instance: opts.Instance, Condition: opts.Condition, StartOffsetNanos: now, Status: "running"}
	for _, dependency := range opts.DependsOn {
		dependency.StageID = id
		r.addDependencyLocked(dependency)
	}
	r.mu.Unlock()
	return id
}

func Dependency(dependsOn StageID, relation string) StageDependency {
	return StageDependency{DependsOnID: dependsOn, Relation: relation}
}

func AddStageDependency(stageID, dependsOnID StageID, relation string) {
	if stageID == 0 || dependsOnID == 0 || stageID == dependsOnID {
		return
	}
	r := stageMeter()
	r.mu.Lock()
	r.addDependencyLocked(StageDependency{StageID: stageID, DependsOnID: dependsOnID, Relation: relation})
	r.mu.Unlock()
}

func (r *stageRecorder) addDependencyLocked(dependency StageDependency) {
	if dependency.StageID == 0 || dependency.DependsOnID == 0 || dependency.StageID == dependency.DependsOnID {
		return
	}
	if dependency.Relation == "" {
		dependency.Relation = StageRelationHard
	}
	for _, existing := range r.dependencies {
		if existing == dependency {
			return
		}
	}
	r.dependencies = append(r.dependencies, dependency)
}

func MarkStageServiceStarted(id StageID) {
	r := stageMeter()
	r.mu.Lock()
	if span := r.spans[id]; span != nil && span.ServiceOffsetNanos == 0 {
		span.ServiceOffsetNanos = time.Since(r.epoch).Nanoseconds()
	}
	r.mu.Unlock()
}

func FinishStage(id StageID, err error) { finishStage(id, stageStatus(err), errorClass(err)) }

func CancelStage(id StageID) { finishStage(id, "canceled", "context-canceled") }

func SkipStage(id StageID) { finishStage(id, "skipped", "") }

func finishStage(id StageID, status, class string) {
	if id == 0 {
		return
	}
	r := stageMeter()
	now := time.Since(r.epoch).Nanoseconds()
	r.mu.Lock()
	if span := r.spans[id]; span != nil && span.Status == "running" {
		span.EndOffsetNanos = now
		span.DurationNanos = now - span.StartOffsetNanos
		span.Status = status
		span.ErrorClass = class
	}
	r.mu.Unlock()
}

func ContextWithStage(ctx context.Context, id StageID) context.Context {
	return context.WithValue(ctx, stageContextKey{}, id)
}

func StageFromContext(ctx context.Context) StageID {
	if ctx == nil {
		return 0
	}
	id, _ := ctx.Value(stageContextKey{}).(StageID)
	return id
}

func RecordStageNetwork(id StageID, uniqueBytes, transferredBytes uint64) {
	updateStageWork(id, func(work *StageWork) {
		work.NetworkUniqueBytes += uniqueBytes
		work.NetworkTransferredBytes += transferredBytes
	})
}

func RecordStageDisk(id StageID, readBytes, writeBytes uint64) {
	updateStageWork(id, func(work *StageWork) { work.DiskReadBytes += readBytes; work.DiskWriteBytes += writeBytes })
}

func RecordStageOutput(id StageID, bytes uint64) {
	updateStageWork(id, func(work *StageWork) { work.OutputBytes += bytes })
}

func RecordStageCPU(id StageID, class, unit string, units uint64) {
	updateStageWork(id, func(work *StageWork) {
		for i := range work.CPU {
			if work.CPU[i].Class == class && work.CPU[i].Unit == unit {
				work.CPU[i].Units += units
				return
			}
		}
		work.CPU = append(work.CPU, WorkCounter{Class: class, Unit: unit, Units: units})
	})
}

func MarkUnattributedStageWork() {
	r := stageMeter()
	r.mu.Lock()
	r.unattributedWork = true
	r.mu.Unlock()
}

func ReconcileStageNetwork(uniqueBytes, transferredBytes uint64) {
	r := stageMeter()
	r.mu.Lock()
	var attributedUnique, attributedTransferred uint64
	for _, work := range r.work {
		attributedUnique += work.NetworkUniqueBytes
		attributedTransferred += work.NetworkTransferredBytes
	}
	if attributedUnique != uniqueBytes || attributedTransferred != transferredBytes {
		r.unattributedWork = true
	}
	r.mu.Unlock()
}

func updateStageWork(id StageID, update func(*StageWork)) {
	r := stageMeter()
	r.mu.Lock()
	if id == 0 || r.spans[id] == nil {
		r.unattributedWork = true
		r.mu.Unlock()
		return
	}
	work := r.work[id]
	if work == nil {
		work = &StageWork{StageID: id}
		r.work[id] = work
	}
	update(work)
	r.mu.Unlock()
}

func SnapshotStages() StageSnapshot {
	r := stageMeter()
	r.mu.Lock()
	defer r.mu.Unlock()
	result := StageSnapshot{Complete: !r.unattributedWork, UnattributedWork: r.unattributedWork, Spans: make([]StageSpan, 0, len(r.spans)), Dependencies: append([]StageDependency(nil), r.dependencies...), Work: make([]StageWork, 0, len(r.work))}
	for _, span := range r.spans {
		copy := *span
		if copy.Status == "running" {
			result.Complete = false
		}
		result.Spans = append(result.Spans, copy)
	}
	for _, work := range r.work {
		copy := *work
		copy.CPU = append([]WorkCounter(nil), work.CPU...)
		sort.Slice(copy.CPU, func(i, j int) bool { return copy.CPU[i].Class < copy.CPU[j].Class })
		result.Work = append(result.Work, copy)
	}
	sort.Slice(result.Spans, func(i, j int) bool { return result.Spans[i].ID < result.Spans[j].ID })
	sort.Slice(result.Dependencies, func(i, j int) bool {
		if result.Dependencies[i].StageID == result.Dependencies[j].StageID {
			return result.Dependencies[i].DependsOnID < result.Dependencies[j].DependsOnID
		}
		return result.Dependencies[i].StageID < result.Dependencies[j].StageID
	})
	sort.Slice(result.Work, func(i, j int) bool { return result.Work[i].StageID < result.Work[j].StageID })
	return result
}

func SnapshotStageDependencies() map[string][]string {
	snapshot := SnapshotStages()
	names := make(map[StageID]string, len(snapshot.Spans))
	for _, span := range snapshot.Spans {
		names[span.ID] = span.Name
	}
	result := make(map[string][]string)
	for _, dependency := range snapshot.Dependencies {
		name, dependencyName := names[dependency.StageID], names[dependency.DependsOnID]
		if name == "" || dependencyName == "" {
			continue
		}
		found := false
		for _, existing := range result[name] {
			if existing == dependencyName {
				found = true
				break
			}
		}
		if !found {
			result[name] = append(result[name], dependencyName)
		}
	}
	for name := range result {
		sort.Strings(result[name])
	}
	// The current floor reader consumes the coarse CASC stage names emitted by
	// reportPreload. Keep that compatibility graph explicit while the detailed
	// ID graph above preserves every repeated and conditional occurrence.
	compatibility := map[string][]string{
		"casc-server-config":     {},
		"casc-cdn-config":        {"casc-server-config"},
		"casc-build-config":      {"casc-server-config"},
		"casc-archives":          {"casc-cdn-config"},
		"casc-encoding":          {"casc-build-config"},
		"casc-root":              {"casc-encoding"},
		"casc-root-selected":     {"casc-build-config"},
		"casc-encoding-selected": {"casc-root-selected"},
		"casc-archives-selected": {"casc-encoding-selected"},
	}
	for name, dependencies := range compatibility {
		result[name] = append([]string(nil), dependencies...)
	}
	return result
}

func stageMeter() *stageRecorder {
	if recorder := stages.Load(); recorder != nil {
		return recorder
	}
	ResetStages()
	return stages.Load()
}

func stageStatus(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "canceled"
	}
	return "error"
}

func errorClass(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "context-canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline-exceeded"
	}
	return "error"
}
