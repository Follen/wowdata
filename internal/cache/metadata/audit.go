package metadata

import "database/sql"

type CacheAudit struct {
	Actor  string
	Path   string
	Bytes  int64
	Reason string
}

func RecordCacheAudit(db *sql.DB, audit CacheAudit) error {
	_, err := db.Exec(`
INSERT INTO cache_audit (actor, path, bytes, reason)
VALUES (?, ?, ?, ?)`,
		audit.Actor,
		audit.Path,
		audit.Bytes,
		audit.Reason,
	)
	return err
}
