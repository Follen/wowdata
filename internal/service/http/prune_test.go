package http

import (
	"context"
	"testing"
)

func TestPruneStateCanDeleteRejectsActivePinnedAndInFlightBuilds(t *testing.T) {
	state := PruneState{
		Active:   map[string]struct{}{"cn/wow/active": {}},
		Pinned:   map[string]struct{}{"cn/wow/pinned": {}},
		InFlight: map[string]struct{}{"cn/wow/in-flight": {}},
	}

	for _, build := range []PruneBuild{
		{Region: "cn", Product: "wow", BuildKey: "active"},
		{Region: "cn", Product: "wow", BuildKey: "pinned"},
		{Region: "cn", Product: "wow", BuildKey: "in-flight"},
	} {
		if state.CanDelete(build) {
			t.Fatalf("CanDelete(%s) = true, want false", build.BuildKey)
		}
	}
}

func TestPruneStateCanDeleteAllowsOldBuild(t *testing.T) {
	state := PruneState{
		Active:   map[string]struct{}{"cn/wow/active": {}},
		Pinned:   map[string]struct{}{"cn/wow/pinned": {}},
		InFlight: map[string]struct{}{"cn/wow/in-flight": {}},
	}

	if !state.CanDelete(PruneBuild{Region: "cn", Product: "wow", BuildKey: "old"}) {
		t.Fatal("CanDelete(old) = false, want true")
	}
}

func TestPrunerPlanReturnsOnlyDeletableBuildsAndRecordsAudit(t *testing.T) {
	store := &fakePruneStore{
		builds: []PruneBuild{
			{Region: "cn", Product: "wow", BuildKey: "active", Path: "cache/active", Bytes: 10},
			{Region: "cn", Product: "wow", BuildKey: "old", Path: "cache/old", Bytes: 20},
		},
		state: PruneState{
			Active: map[string]struct{}{"cn/wow/active": {}},
		},
	}
	pruner := Pruner{Store: store, Actor: "test-pruner"}

	plan, err := pruner.Plan(context.Background())

	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Delete) != 1 || plan.Delete[0].BuildKey != "old" {
		t.Fatalf("plan delete = %#v, want old only", plan.Delete)
	}
	if len(store.audits) != 1 {
		t.Fatalf("audit count = %d, want 1", len(store.audits))
	}
	if store.audits[0].Path != "cache/old" || store.audits[0].Reason != "prune_plan" {
		t.Fatalf("audit = %#v, want old prune_plan", store.audits[0])
	}
}

type fakePruneStore struct {
	builds []PruneBuild
	state  PruneState
	audits []PruneAudit
}

func (f *fakePruneStore) PruneCandidates(context.Context) ([]PruneBuild, error) {
	return f.builds, nil
}

func (f *fakePruneStore) PruneState(context.Context) (PruneState, error) {
	return f.state, nil
}

func (f *fakePruneStore) RecordPruneAudit(_ context.Context, audit PruneAudit) error {
	f.audits = append(f.audits, audit)
	return nil
}
