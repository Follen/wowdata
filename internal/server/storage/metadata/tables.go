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

type TableCatalogLookup struct {
	Region   string
	Product  string
	Locale   string
	BuildKey string
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

func ListValidMaterializedTables(ctx context.Context, db *sql.DB, lookup TableCatalogLookup) ([]MaterializedTable, error) {
	rows, err := db.QueryContext(ctx, `
SELECT t.region, t.product, t.locale, t.build_key, t.table_name,
  t.db2_file_data_id, t.dbd_hash, t.decoder_version, t.materializer_version,
  t.parquet_path, t.row_count, t.state, t.error
FROM server_materialized_tables t
JOIN (
  SELECT table_name, MAX(updated_seq) AS updated_seq
  FROM server_materialized_tables
  WHERE region = ? AND product = ? AND locale = ? AND build_key = ? AND state = ?
  GROUP BY table_name
) latest ON latest.table_name = t.table_name AND latest.updated_seq = t.updated_seq
WHERE t.region = ? AND t.product = ? AND t.locale = ? AND t.build_key = ? AND t.state = ?
ORDER BY t.table_name`,
		lookup.Region,
		lookup.Product,
		lookup.Locale,
		lookup.BuildKey,
		StateValid,
		lookup.Region,
		lookup.Product,
		lookup.Locale,
		lookup.BuildKey,
		StateValid,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []MaterializedTable
	for rows.Next() {
		var table MaterializedTable
		if err := rows.Scan(
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
		); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}
	return tables, rows.Err()
}
