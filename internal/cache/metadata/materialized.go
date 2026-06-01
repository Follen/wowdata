package metadata

import (
	"database/sql"
)

const (
	StateValid = "valid"
	StateStale = "stale"
)

type MaterializedTable struct {
	Region              string
	Product             string
	BuildKey            string
	BuildName           string
	Locale              string
	TableName           string
	DB2FileDataID       int
	DBDDefinitionHash   string
	DecoderVersion      string
	MaterializerVersion string
	ParquetPath         string
	RowCount            int
	State               string
}

func UpsertMaterializedTable(db *sql.DB, table MaterializedTable) error {
	_, err := db.Exec(`
INSERT INTO materialized_tables (
  region, product, build_key, build_name, locale, table_name,
  db2_file_data_id, dbd_definition_hash, decoder_version, materializer_version,
  parquet_path, row_count, state, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(region, product, build_key, locale, table_name) DO UPDATE SET
  build_name = excluded.build_name,
  db2_file_data_id = excluded.db2_file_data_id,
  dbd_definition_hash = excluded.dbd_definition_hash,
  decoder_version = excluded.decoder_version,
  materializer_version = excluded.materializer_version,
  parquet_path = excluded.parquet_path,
  row_count = excluded.row_count,
  state = excluded.state,
  updated_at = CURRENT_TIMESTAMP`,
		table.Region,
		table.Product,
		table.BuildKey,
		table.BuildName,
		table.Locale,
		table.TableName,
		table.DB2FileDataID,
		table.DBDDefinitionHash,
		table.DecoderVersion,
		table.MaterializerVersion,
		table.ParquetPath,
		table.RowCount,
		table.State,
	)
	return err
}

func GetMaterializedTable(db *sql.DB, region, product, buildKey, locale, tableName string) (MaterializedTable, error) {
	var table MaterializedTable
	err := db.QueryRow(`
SELECT region, product, build_key, build_name, locale, table_name,
  db2_file_data_id, dbd_definition_hash, decoder_version, materializer_version,
  parquet_path, row_count, state
FROM materialized_tables
WHERE region = ? AND product = ? AND build_key = ? AND locale = ? AND table_name = ?`,
		region,
		product,
		buildKey,
		locale,
		tableName,
	).Scan(
		&table.Region,
		&table.Product,
		&table.BuildKey,
		&table.BuildName,
		&table.Locale,
		&table.TableName,
		&table.DB2FileDataID,
		&table.DBDDefinitionHash,
		&table.DecoderVersion,
		&table.MaterializerVersion,
		&table.ParquetPath,
		&table.RowCount,
		&table.State,
	)
	return table, err
}

func LatestMaterializedTable(db *sql.DB, region, product, locale, tableName string) (MaterializedTable, error) {
	var table MaterializedTable
	err := db.QueryRow(`
SELECT region, product, build_key, build_name, locale, table_name,
  db2_file_data_id, dbd_definition_hash, decoder_version, materializer_version,
  parquet_path, row_count, state
FROM materialized_tables
WHERE region = ? AND product = ? AND locale = ? AND table_name = ? AND state = ?
ORDER BY updated_at DESC
LIMIT 1`,
		region,
		product,
		locale,
		tableName,
		StateValid,
	).Scan(
		&table.Region,
		&table.Product,
		&table.BuildKey,
		&table.BuildName,
		&table.Locale,
		&table.TableName,
		&table.DB2FileDataID,
		&table.DBDDefinitionHash,
		&table.DecoderVersion,
		&table.MaterializerVersion,
		&table.ParquetPath,
		&table.RowCount,
		&table.State,
	)
	return table, err
}

func MarkMaterializedTableStale(db *sql.DB, region, product, buildKey, locale, tableName string) error {
	_, err := db.Exec(`
UPDATE materialized_tables
SET state = ?, updated_at = CURRENT_TIMESTAMP
WHERE region = ? AND product = ? AND build_key = ? AND locale = ? AND table_name = ?`,
		StateStale,
		region,
		product,
		buildKey,
		locale,
		tableName,
	)
	return err
}

func (t MaterializedTable) FingerprintMatches(want MaterializedTable) bool {
	return t.Region == want.Region &&
		t.Product == want.Product &&
		t.BuildKey == want.BuildKey &&
		t.Locale == want.Locale &&
		t.TableName == want.TableName &&
		t.DB2FileDataID == want.DB2FileDataID &&
		t.DBDDefinitionHash == want.DBDDefinitionHash &&
		t.DecoderVersion == want.DecoderVersion &&
		t.MaterializerVersion == want.MaterializerVersion
}
