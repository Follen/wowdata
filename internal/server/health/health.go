package health

import "context"

const (
	StateReady     = "ready"
	StatePreparing = "preparing"
	StateNoBuild   = "no_build"
	StateFailed    = "failed"
	StateStale     = "stale"
)

type Provider interface {
	HealthSnapshot(context.Context) (Snapshot, error)
}

type Snapshot struct {
	Liveness     Liveness        `json:"liveness"`
	Readiness    Readiness       `json:"readiness"`
	Matrix       Matrix          `json:"matrix"`
	Memory       Memory          `json:"memory"`
	Storage      Storage         `json:"storage"`
	Contexts     []ContextStatus `json:"contexts"`
	RecentErrors []string        `json:"recentErrors,omitempty"`
}

type Liveness struct {
	OK bool `json:"ok"`
}

type Readiness struct {
	OK                   bool `json:"ok"`
	RequiredTargetsReady int  `json:"requiredTargetsReady"`
	RequiredTargetsTotal int  `json:"requiredTargetsTotal"`
}

type Matrix struct {
	TargetsTotal int `json:"targetsTotal"`
	Ready        int `json:"ready"`
	Preparing    int `json:"preparing"`
	NoBuild      int `json:"noBuild,omitempty"`
	Failed       int `json:"failed,omitempty"`
	Stale        int `json:"stale,omitempty"`
}

type Memory struct {
	RSSBytes          int64 `json:"rssBytes,omitempty"`
	HeapAllocBytes    int64 `json:"heapAllocBytes,omitempty"`
	MemorySoftLimitMB int   `json:"memorySoftLimitMB"`
	MemoryHardLimitMB int   `json:"memoryHardLimitMB"`
}

type Storage struct {
	MetadataDBBytes int64 `json:"metadataDBBytes"`
	RawCacheBytes   int64 `json:"rawCacheBytes,omitempty"`
	DB2Bytes        int64 `json:"db2Bytes,omitempty"`
	ArtifactBytes   int64 `json:"artifactBytes,omitempty"`
}

type ContextStatus struct {
	Label          string `json:"label"`
	Region         string `json:"region,omitempty"`
	Product        string `json:"product,omitempty"`
	Locale         string `json:"locale,omitempty"`
	State          string `json:"state"`
	Strict         bool   `json:"strict,omitempty"`
	ActiveBuild    string `json:"activeBuild,omitempty"`
	CandidateBuild     string `json:"candidateBuild,omitempty"`
	CandidateBuildName string `json:"candidateBuildName,omitempty"`
	DB2Ready       bool   `json:"db2Ready,omitempty"`
	ListfileReady  bool   `json:"listfileReady,omitempty"`
	CASCReady      bool   `json:"cascReady,omitempty"`
	PrepareCurrent int    `json:"prepareCurrent,omitempty"`
	PrepareTotal   int    `json:"prepareTotal,omitempty"`
	Error          string `json:"error,omitempty"`
}

type Input struct {
	Targets      []TargetInput
	Memory       Memory
	Storage      Storage
	RecentErrors []string
}

type TargetInput struct {
	Label          string
	Region         string
	Product        string
	Locale         string
	State          string
	Strict         bool
	Unsupported    bool
	ActiveBuild    string
	CandidateBuild     string
	CandidateBuildName string
	DB2Ready       bool
	ListfileReady  bool
	CASCReady      bool
	PrepareCurrent int
	PrepareTotal   int
	Error          string
}

type StaticProvider struct {
	Snapshot Snapshot
}

func (p StaticProvider) HealthSnapshot(context.Context) (Snapshot, error) {
	return p.Snapshot, nil
}

func BuildSnapshot(input Input) Snapshot {
	snapshot := Snapshot{
		Liveness:     Liveness{OK: true},
		Memory:       input.Memory,
		Storage:      input.Storage,
		Contexts:     make([]ContextStatus, 0, len(input.Targets)),
		RecentErrors: append([]string{}, input.RecentErrors...),
	}

	for _, target := range input.Targets {
		state := target.State
		if state == "" {
			state = StatePreparing
		}
		contextStatus := ContextStatus{
			Label:          target.Label,
			Region:         target.Region,
			Product:        target.Product,
			Locale:         target.Locale,
			State:          state,
			Strict:         target.Strict,
			ActiveBuild:    target.ActiveBuild,
			CandidateBuild: target.CandidateBuild,
			CandidateBuildName: target.CandidateBuildName,
			DB2Ready:       target.DB2Ready,
			ListfileReady:  target.ListfileReady,
			CASCReady:      target.CASCReady,
			PrepareCurrent: target.PrepareCurrent,
			PrepareTotal:   target.PrepareTotal,
			Error:          target.Error,
		}
		snapshot.Contexts = append(snapshot.Contexts, contextStatus)
		snapshot.Matrix.TargetsTotal++

		switch state {
		case StateReady:
			snapshot.Matrix.Ready++
		case StatePreparing:
			snapshot.Matrix.Preparing++
		case StateNoBuild:
			snapshot.Matrix.NoBuild++
		case StateFailed:
			snapshot.Matrix.Failed++
		case StateStale:
			snapshot.Matrix.Stale++
		}

		if readinessRequired(target, state) {
			snapshot.Readiness.RequiredTargetsTotal++
			if state == StateReady {
				snapshot.Readiness.RequiredTargetsReady++
			}
		}
	}

	snapshot.Readiness.OK = snapshot.Readiness.RequiredTargetsReady == snapshot.Readiness.RequiredTargetsTotal
	return snapshot
}

func readinessRequired(target TargetInput, state string) bool {
	if target.Strict {
		return true
	}
	return !target.Unsupported && state != StateNoBuild
}
