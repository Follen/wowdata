package metadata

import (
	"path/filepath"
	"testing"
)

func TestBuildMetadataHelpersActivateBuildAtomically(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "metadata.sqlite"), "../../../migrations/sqlite")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := UpsertBuild(db, Build{Region: "cn", Product: "wow", BuildKey: "old", BuildName: "12.0.0.1", Active: true, Ready: true}); err != nil {
		t.Fatalf("upsert old: %v", err)
	}
	if err := UpsertBuild(db, Build{Region: "cn", Product: "wow", BuildKey: "new", BuildName: "12.0.0.2"}); err != nil {
		t.Fatalf("upsert new: %v", err)
	}

	if err := ActivateBuild(db, "cn", "wow", "new"); err != nil {
		t.Fatalf("activate new: %v", err)
	}

	oldBuild, err := GetBuild(db, "cn", "wow", "old")
	if err != nil {
		t.Fatalf("get old: %v", err)
	}
	newBuild, err := GetBuild(db, "cn", "wow", "new")
	if err != nil {
		t.Fatalf("get new: %v", err)
	}
	if oldBuild.Active {
		t.Fatal("old build active = true, want false")
	}
	if !newBuild.Active {
		t.Fatalf("new build active = false, want true")
	}
}

func TestActivateBuildMissingBuildKeepsExistingActiveBuild(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "metadata.sqlite"), "../../../migrations/sqlite")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := UpsertBuild(db, Build{Region: "cn", Product: "wow", BuildKey: "old", BuildName: "12.0.0.1", Active: true, Ready: true}); err != nil {
		t.Fatalf("upsert old: %v", err)
	}

	if err := ActivateBuild(db, "cn", "wow", "missing"); err == nil {
		t.Fatal("ActivateBuild missing build error = nil")
	}

	oldBuild, err := GetBuild(db, "cn", "wow", "old")
	if err != nil {
		t.Fatalf("get old: %v", err)
	}
	if !oldBuild.Active {
		t.Fatalf("old build active = false after missing activation, want true")
	}
}

func TestActivateBuildOnlyTogglesActiveMetadata(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "metadata.sqlite"), "../../../migrations/sqlite")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := UpsertBuild(db, Build{Region: "cn", Product: "wow", BuildKey: "old", BuildName: "12.0.0.1", Active: true, Ready: true}); err != nil {
		t.Fatalf("upsert old: %v", err)
	}
	if err := UpsertBuild(db, Build{Region: "cn", Product: "wow", BuildKey: "new", BuildName: "12.0.0.2", Ready: false, Error: "prepare failed"}); err != nil {
		t.Fatalf("upsert new: %v", err)
	}

	if err := ActivateBuild(db, "cn", "wow", "new"); err != nil {
		t.Fatalf("activate new: %v", err)
	}

	oldBuild, err := GetBuild(db, "cn", "wow", "old")
	if err != nil {
		t.Fatalf("get old: %v", err)
	}
	newBuild, err := GetBuild(db, "cn", "wow", "new")
	if err != nil {
		t.Fatalf("get new: %v", err)
	}
	if oldBuild.Active {
		t.Fatal("old build active = true, want false")
	}
	if !newBuild.Active {
		t.Fatal("new build active = false, want true")
	}
	if newBuild.Ready {
		t.Fatalf("new build ready = true, want preserved false")
	}
	if newBuild.Error != "prepare failed" {
		t.Fatalf("new build error = %q, want preserved prepare failed", newBuild.Error)
	}
}
