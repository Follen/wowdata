package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"wowdata/internal/storage"
)

func TestValidNPMVersion(t *testing.T) {
	for _, version := range []string{"latest", "1.2.3", "1.2.3-beta.1", "1.2.3+build.4"} {
		if !validNPMVersion(version) {
			t.Errorf("validNPMVersion(%q) = false", version)
		}
	}
	for _, version := range []string{"", "^1.2.3", "latest & whoami", "1.2"} {
		if validNPMVersion(version) {
			t.Errorf("validNPMVersion(%q) = true", version)
		}
	}
}

func TestDoctorStorageChecksReportTargetAndStaleFiles(t *testing.T) {
	layout, err := storage.Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	_, err = layout.SaveBuild(storage.BuildSnapshot{Source: "remote", Region: "cn", Product: "wow", Locale: "zhCN", Version: "12.0.0.12345", BuildConfigKey: "build-key"})
	if err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(layout.Locks, "stale.lock")
	if err := os.WriteFile(stalePath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stalePath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	snapshot, err := latestBuildSnapshot(layout)
	if err != nil || snapshot == nil || snapshot.Product != "wow" {
		t.Fatalf("latestBuildSnapshot = %#v, %v", snapshot, err)
	}
	checks := doctorResidueChecks(layout)
	if checks[0].Name != "locks" || checks[0].Status != "error" || checks[0].ProblemCode != "stale_locks" {
		t.Fatalf("lock check = %#v", checks[0])
	}
	if checks[1].Name != "temporary_files" || checks[1].Status != "ok" {
		t.Fatalf("temporary check = %#v", checks[1])
	}
}
