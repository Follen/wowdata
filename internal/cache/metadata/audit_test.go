package metadata

import (
	"path/filepath"
	"testing"
)

func TestRecordCacheAuditWritesAuditRow(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "metadata.sqlite"), "../../../migrations/sqlite")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := RecordCacheAudit(db, CacheAudit{
		Actor:  "test-pruner",
		Path:   "cache/old",
		Bytes:  42,
		Reason: "prune_plan",
	}); err != nil {
		t.Fatalf("record audit: %v", err)
	}

	var actor, path, reason string
	var bytes int64
	if err := db.QueryRow(`SELECT actor, path, bytes, reason FROM cache_audit`).Scan(&actor, &path, &bytes, &reason); err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if actor != "test-pruner" || path != "cache/old" || bytes != 42 || reason != "prune_plan" {
		t.Fatalf("audit row = actor:%q path:%q bytes:%d reason:%q", actor, path, bytes, reason)
	}
}
