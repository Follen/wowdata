package cascindex

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
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

func TestReplaceIndexIsScopedByBuild(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	us := SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-us"}
	cn := SourceKey{Region: "cn", Product: "wow", Locale: "zhCN", BuildKey: "build-cn"}

	if err := ReplaceIndexForSource(ctx, db, us, "source-us",
		[]RootMapping{{FileDataID: 1, ContentKey: "content-us"}},
		[]EncodingMapping{{ContentKey: "content-us", EncodingKey: "encoding-us", Size: 10}},
		[]ArchiveMapping{{EncodingKey: "encoding-us", ArchiveKey: "archive-us", Offset: 20, Size: 10}},
	); err != nil {
		t.Fatalf("replace us index: %v", err)
	}
	if err := ReplaceIndexForSource(ctx, db, cn, "source-cn",
		[]RootMapping{{FileDataID: 1, ContentKey: "content-cn"}},
		[]EncodingMapping{{ContentKey: "content-cn", EncodingKey: "encoding-cn", Size: 11}},
		[]ArchiveMapping{{EncodingKey: "encoding-cn", ArchiveKey: "archive-cn", Offset: 21, Size: 11}},
	); err != nil {
		t.Fatalf("replace cn index: %v", err)
	}

	gotUS, err := ResolveFileDataIDForSource(ctx, db, us, 1)
	if err != nil {
		t.Fatalf("resolve us: %v", err)
	}
	gotCN, err := ResolveFileDataIDForSource(ctx, db, cn, 1)
	if err != nil {
		t.Fatalf("resolve cn: %v", err)
	}
	if gotUS.EncodingKey != "encoding-us" || gotCN.EncodingKey != "encoding-cn" {
		t.Fatalf("scoped indexes overwritten: us=%#v cn=%#v", gotUS, gotCN)
	}
}

func TestHasUsableIndexForSourceRequiresAllMappingTables(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	source := SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-us"}

	ready, err := HasUsableIndexForSource(ctx, db, source)
	if err != nil {
		t.Fatalf("HasUsableIndexForSource before replace: %v", err)
	}
	if ready {
		t.Fatal("HasUsableIndexForSource before replace = true, want false")
	}

	if err := ReplaceIndexForSource(ctx, db, source, "source-us",
		[]RootMapping{{FileDataID: 1, ContentKey: "content-us"}},
		[]EncodingMapping{{ContentKey: "content-us", EncodingKey: "encoding-us", Size: 10}},
		nil,
	); err != nil {
		t.Fatalf("replace partial index: %v", err)
	}
	ready, err = HasUsableIndexForSource(ctx, db, source)
	if err != nil {
		t.Fatalf("HasUsableIndexForSource partial: %v", err)
	}
	if ready {
		t.Fatal("HasUsableIndexForSource partial = true, want false")
	}

	if err := ReplaceIndexForSource(ctx, db, source, "source-us",
		[]RootMapping{{FileDataID: 1, ContentKey: "content-us"}},
		[]EncodingMapping{{ContentKey: "other-content", EncodingKey: "encoding-us", Size: 10}},
		[]ArchiveMapping{{EncodingKey: "encoding-us", ArchiveKey: "archive-us", Offset: 20, Size: 10}},
	); err != nil {
		t.Fatalf("replace mismatched index: %v", err)
	}
	ready, err = HasUsableIndexForSource(ctx, db, source)
	if err != nil {
		t.Fatalf("HasUsableIndexForSource mismatched: %v", err)
	}
	if ready {
		t.Fatal("HasUsableIndexForSource mismatched = true, want false")
	}

	if err := ReplaceIndexForSource(ctx, db, source, "source-us",
		[]RootMapping{{FileDataID: 1, ContentKey: "content-us"}},
		[]EncodingMapping{{ContentKey: "content-us", EncodingKey: "encoding-us", Size: 10}},
		[]ArchiveMapping{{EncodingKey: "encoding-us", ArchiveKey: "archive-us", Offset: 20, Size: 10}},
	); err != nil {
		t.Fatalf("replace complete index: %v", err)
	}
	ready, err = HasUsableIndexForSource(ctx, db, source)
	if err != nil {
		t.Fatalf("HasUsableIndexForSource complete: %v", err)
	}
	if !ready {
		t.Fatal("HasUsableIndexForSource complete = false, want true")
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

func TestResolveFileDataIDSpansAllowsCascMultiplicity(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	err := ReplaceIndex(ctx, db, "build-1",
		[]RootMapping{
			{FileDataID: 7, ContentKey: "content-a"},
			{FileDataID: 7, ContentKey: "content-b"},
		},
		[]EncodingMapping{
			{ContentKey: "content-a", EncodingKey: "encoding-a1", Size: 100},
			{ContentKey: "content-a", EncodingKey: "encoding-a2", Size: 101},
			{ContentKey: "content-b", EncodingKey: "encoding-b1", Size: 102},
		},
		[]ArchiveMapping{
			{EncodingKey: "encoding-a1", ArchiveKey: "archive-a1", Offset: 10, Size: 100},
			{EncodingKey: "encoding-a1", ArchiveKey: "archive-a1b", Offset: 20, Size: 100},
			{EncodingKey: "encoding-a2", ArchiveKey: "archive-a2", Offset: 30, Size: 101},
			{EncodingKey: "encoding-b1", ArchiveKey: "archive-b1", Offset: 40, Size: 102},
		},
	)
	if err != nil {
		t.Fatalf("ReplaceIndex with CASC multiplicity: %v", err)
	}

	want := []ArchiveSpan{
		{FileDataID: 7, ContentKey: "content-a", EncodingKey: "encoding-a1", ArchiveKey: "archive-a1", Offset: 10, Size: 100},
		{FileDataID: 7, ContentKey: "content-a", EncodingKey: "encoding-a1", ArchiveKey: "archive-a1b", Offset: 20, Size: 100},
		{FileDataID: 7, ContentKey: "content-a", EncodingKey: "encoding-a2", ArchiveKey: "archive-a2", Offset: 30, Size: 101},
		{FileDataID: 7, ContentKey: "content-b", EncodingKey: "encoding-b1", ArchiveKey: "archive-b1", Offset: 40, Size: 102},
	}
	got, err := ResolveFileDataIDSpans(ctx, db, 7)
	if err != nil {
		t.Fatalf("ResolveFileDataIDSpans: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveFileDataIDSpans() = %#v, want %#v", got, want)
	}

	first, err := ResolveFileDataID(ctx, db, 7)
	if err != nil {
		t.Fatalf("ResolveFileDataID: %v", err)
	}
	if first != want[0] {
		t.Fatalf("ResolveFileDataID() = %#v, want deterministic first %#v", first, want[0])
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
