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
	if !newBuild.Active || !newBuild.Ready {
		t.Fatalf("new build = %#v, want active and ready", newBuild)
	}
}
