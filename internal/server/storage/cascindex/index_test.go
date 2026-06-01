package cascindex

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestResolveFileDataIDToArchiveSpan(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	err := ReplaceIndex(ctx, db, "build-1",
		[]RootMapping{{FileDataID: 12345, ContentKey: "content-a"}},
		[]EncodingMapping{{ContentKey: "content-a", EncodingKey: "encoding-a", Size: 2048}},
		[]ArchiveMapping{{EncodingKey: "encoding-a", ArchiveKey: "archive-a", Offset: 4096, Size: 2048}},
	)
	if err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	got, err := ResolveFileDataID(ctx, db, 12345)
	if err != nil {
		t.Fatalf("ResolveFileDataID: %v", err)
	}
	want := ArchiveSpan{
		FileDataID:  12345,
		ContentKey:  "content-a",
		EncodingKey: "encoding-a",
		ArchiveKey:  "archive-a",
		Offset:      4096,
		Size:        2048,
	}
	if got != want {
		t.Fatalf("ResolveFileDataID() = %#v, want %#v", got, want)
	}
}

func TestSourceVersionChangeMarksIndexStale(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if err := ReplaceIndex(ctx, db, "build-1",
		[]RootMapping{{FileDataID: 1, ContentKey: "content-1"}},
		[]EncodingMapping{{ContentKey: "content-1", EncodingKey: "encoding-1", Size: 10}},
		[]ArchiveMapping{{EncodingKey: "encoding-1", ArchiveKey: "archive-1", Offset: 20, Size: 10}},
	); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	state, err := IndexState(ctx, db)
	if err != nil {
		t.Fatalf("IndexState before source change: %v", err)
	}
	if state != "valid" {
		t.Fatalf("IndexState before source change = %q, want valid", state)
	}

	changed, err := UpsertSourceVersion(ctx, db, "build-2")
	if err != nil {
		t.Fatalf("UpsertSourceVersion changed version: %v", err)
	}
	if !changed {
		t.Fatal("UpsertSourceVersion changed = false, want true")
	}

	state, err = IndexState(ctx, db)
	if err != nil {
		t.Fatalf("IndexState after source change: %v", err)
	}
	if state != "stale" {
		t.Fatalf("IndexState after source change = %q, want stale", state)
	}

	got, err := ResolveFileDataID(ctx, db, 1)
	if err != nil {
		t.Fatalf("ResolveFileDataID after source change: %v", err)
	}
	if got.ArchiveKey != "archive-1" {
		t.Fatalf("source version change deleted existing index rows, got archive key %q", got.ArchiveKey)
	}

	changed, err = UpsertSourceVersion(ctx, db, "build-2")
	if err != nil {
		t.Fatalf("UpsertSourceVersion same version: %v", err)
	}
	if changed {
		t.Fatal("UpsertSourceVersion same version changed = true, want false")
	}
}

func TestMissingFileDataIDReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if err := ReplaceIndex(ctx, db, "build-1", nil, nil, nil); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	_, err := ResolveFileDataID(ctx, db, 404)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ResolveFileDataID missing error = %v, want ErrNotFound", err)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cascindex.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})
	return db
}
