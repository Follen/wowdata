//go:build !wowdata_duckdb || !cgo

package duckdb

import "database/sql"

func duckDBAvailable() bool {
	return false
}

func openDuckDB(path string) (*sql.DB, error) {
	return nil, ErrUnavailable
}
