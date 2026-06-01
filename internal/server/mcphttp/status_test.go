package mcphttp

import (
	"context"
	"reflect"
	"testing"

	"wowdata/internal/server/health"
)

func TestWowStatusUsesSameHealthSnapshotAsHTTPHealth(t *testing.T) {
	provider := &fakeHealthProvider{
		snapshot: health.Snapshot{
			Liveness: health.Liveness{OK: true},
			Readiness: health.Readiness{
				OK:                   true,
				RequiredTargetsReady: 1,
				RequiredTargetsTotal: 1,
			},
			Matrix:  health.Matrix{TargetsTotal: 1, Ready: 1},
			Memory:  health.Memory{MemorySoftLimitMB: 4096, MemoryHardLimitMB: 8192},
			Storage: health.Storage{MetadataDBBytes: 1},
			Contexts: []health.ContextStatus{{
				Label: "CN Retail",
				State: health.StateReady,
			}},
		},
	}

	httpSnapshot, err := HealthSnapshot(context.Background(), provider)
	if err != nil {
		t.Fatalf("HTTP health snapshot: %v", err)
	}
	statusSnapshot, err := WowStatus(context.Background(), provider)
	if err != nil {
		t.Fatalf("wow_status snapshot: %v", err)
	}

	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}
	if !reflect.DeepEqual(statusSnapshot, httpSnapshot) {
		t.Fatalf("wow_status snapshot = %#v, want exact HTTP snapshot %#v", statusSnapshot, httpSnapshot)
	}
}

type fakeHealthProvider struct {
	snapshot health.Snapshot
	calls    int
}

func (f *fakeHealthProvider) HealthSnapshot(context.Context) (health.Snapshot, error) {
	f.calls++
	return f.snapshot, nil
}
