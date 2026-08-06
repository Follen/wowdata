package resource

import (
	"context"
	"errors"
	"testing"
)

func TestStageRecorderPreservesIDsWavesDependenciesAndStatus(t *testing.T) {
	ResetStages()
	root := StartStage("root", StageOptions{Wave: StageWaveInitial})
	FinishStage(root, nil)
	first := StartStage("selected", StageOptions{ParentID: root, Wave: StageWaveInitial, Instance: "1", DependsOn: []StageDependency{Dependency(root, StageRelationHard)}})
	FinishStage(first, errors.New("failed"))
	second := StartStage("selected", StageOptions{ParentID: root, Wave: StageWaveResourceSecondWave, Instance: "2", Condition: "batch-failed", DependsOn: []StageDependency{Dependency(first, StageRelationFallback)}})
	FinishStage(second, context.Canceled)

	snapshot := SnapshotStages()
	if !snapshot.Complete || len(snapshot.Spans) != 3 || len(snapshot.Dependencies) != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if first == second || snapshot.Spans[1].Status != "error" || snapshot.Spans[2].Status != "canceled" {
		t.Fatalf("stages = %#v", snapshot.Spans)
	}
	if snapshot.Dependencies[1].Relation != StageRelationFallback {
		t.Fatalf("dependencies = %#v", snapshot.Dependencies)
	}
	if got := SnapshotStageDependencies()["selected"]; len(got) != 2 || got[0] != "root" || got[1] != "selected" {
		t.Fatalf("compat dependencies = %#v", got)
	}
}

func TestStageRecorderIncompleteForRunningOrUnattributedWork(t *testing.T) {
	ResetStages()
	id := StartStage("download", StageOptions{})
	if SnapshotStages().Complete {
		t.Fatal("running stage must make snapshot incomplete")
	}
	FinishStage(id, nil)
	RecordStageNetwork(id, 5, 8)
	ReconcileStageNetwork(6, 8)
	if snapshot := SnapshotStages(); snapshot.Complete || !snapshot.UnattributedWork {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestFinishStageIsIdempotent(t *testing.T) {
	ResetStages()
	id := StartStage("once", StageOptions{})
	FinishStage(id, nil)
	FinishStage(id, errors.New("late"))
	span := SnapshotStages().Spans[0]
	if span.Status != "ok" || span.ErrorClass != "" {
		t.Fatalf("span = %#v", span)
	}
}
