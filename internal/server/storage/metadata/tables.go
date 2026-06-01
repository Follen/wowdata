package metadata

import (
	"context"
	"database/sql"
)

type TableKey struct {
	Region    string
	Product   string
	Locale    string
	BuildKey  string
	TableName string
}

type TableLookup struct {
	Region    string
	Product   string
	Locale    string
	TableName string
}

type MaterializedTable struct {
	Key                 TableKey
	DB2FileDataID       int
	DBDHash             string
	DecoderVersion      string
	MaterializerVersion string
	ParquetPath         string
	RowCount            int
	State               string
	Error               string
}

func UpsertMaterializedTable(ctx context.Context, db *sql.DB, table MaterializedTable) error {
	state := table.State
	if state == "" {
		state = StatePreparing
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var nextSeq int64
	if _, err = tx.ExecContext(ctx, `
INSERT INTO server_metadata_sequences(name, value)
VALUES ('server_materialized_tables', 1)
ON CONFLICT(name) DO UPDATE SET value = value + 1`); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `
SELECT value FROM server_metadata_sequences WHERE name = 'server_materialized_tables'`).Scan(&nextSeq); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO server_materialized_tables (
  region, product, locale, build_key, table_name,
  db2_file_data_id, dbd_hash, decoder_version, materializer_version,
  parquet_path, row_count, state, error, updated_seq, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(region, product, locale, build_key, table_name) DO UPDATE SET
  db2_file_data_id = excluded.db2_file_data_id,
  dbd_hash = excluded.dbd_hash,
  decoder_version = excluded.decoder_version,
  materializer_version = excluded.materializer_version,
  parquet_path = excluded.parquet_path,
  row_count = excluded.row_count,
  state = excluded.state,
  error = excluded.error,
  updated_seq = excluded.updated_seq,
  updated_at = CURRENT_TIMESTAMP`,
		table.Key.Region,
		table.Key.Product,
		table.Key.Locale,
		table.Key.BuildKey,
		table.Key.TableName,
		table.DB2FileDataID,
		table.DBDHash,
		table.DecoderVersion,
		table.MaterializerVersion,
		table.ParquetPath,
		table.RowCount,
		state,
		table.Error,
		nextSeq,
	); err != nil {
		return err
	}
	err = tx.Commit()
	return err
}

func MarkMaterializedTableState(ctx context.Context, db *sql.DB, key TableKey, state string, message string) error {
	_, err := db.ExecContext(ctx, `
UPDATE server_materialized_tables
SET state = ?, error = ?, updated_at = CURRENT_TIMESTAMP
WHERE region = ? AND product = ? AND locale = ? AND build_key = ? AND table_name = ?`,
		state,
		message,
		key.Region,
		key.Product,
		key.Locale,
		key.BuildKey,
		key.TableName,
	)
	return err
}

func LatestValidMaterializedTable(ctx context.Context, db *sql.DB, lookup TableLookup) (MaterializedTable, error) {
	var table MaterializedTable
	err := db.QueryRowContext(ctx, `
SELECT region, product, locale, build_key, table_name,
  db2_file_data_id, dbd_hash, decoder_version, materializer_version,
  parquet_path, row_count, state, error
FROM server_materialized_tables
WHERE region = ? AND product = ? AND locale = ? AND table_name = ? AND state = ?
ORDER BY updated_seq DESC
LIMIT 1`,
		lookup.Region,
		lookup.Product,
		lookup.Locale,
		lookup.TableName,
		StateValid,
	).Scan(
		&table.Key.Region,
		&table.Key.Product,
		&table.Key.Locale,
		&table.Key.BuildKey,
		&table.Key.TableName,
		&table.DB2FileDataID,
		&table.DBDHash,
		&table.DecoderVersion,
		&table.MaterializerVersion,
		&table.ParquetPath,
		&table.RowCount,
		&table.State,
		&table.Error,
	)
	return table, err
}
