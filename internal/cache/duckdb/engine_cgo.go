//go:build cgo

package duckdb

import (
	"database/sql"

	_ "github.com/marcboeker/go-duckdb/v2"
)

func duckDBAvailable() bool {
	return true
}

func openDuckDB(path string) (*sql.DB, error) {
	return sql.Open("duckdb", path)
}
