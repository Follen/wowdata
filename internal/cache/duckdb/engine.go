package duckdb

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
)

var ErrUnavailable = errors.New("duckdb query engine unavailable")

type Engine struct {
	path string
	db   *sql.DB
}

func NewEngine(path string) *Engine {
	return &Engine{path: path}
}

func (e *Engine) Available() bool {
	return duckDBAvailable()
}

func (e *Engine) Open() error {
	if !duckDBAvailable() {
		return ErrUnavailable
	}
	if err := ensureDuckDBParentDir(e.path); err != nil {
		return err
	}
	db, err := openDuckDB(e.path)
	if err != nil {
		return err
	}
	e.db = db
	return nil
}

func (e *Engine) Close() error {
	if e == nil || e.db == nil {
		return nil
	}
	err := e.db.Close()
	e.db = nil
	return err
}

func (e *Engine) QueryParquet(ctx context.Context, query string, args []interface{}) ([]map[string]interface{}, error) {
	if e == nil || !e.Available() {
		return nil, ErrUnavailable
	}
	if e.db == nil {
		if err := e.Open(); err != nil {
			return nil, err
		}
	}
	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	results := make([]map[string]interface{}, 0)
	for rows.Next() {
		values := make([]interface{}, len(columns))
		dest := make([]interface{}, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		row := make(map[string]interface{}, len(columns))
		for i, column := range columns {
			row[column] = values[i]
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func ensureDuckDBParentDir(path string) error {
	if path == "" {
		return nil
	}
	return os.MkdirAll(filepath.Dir(path), 0755)
}
