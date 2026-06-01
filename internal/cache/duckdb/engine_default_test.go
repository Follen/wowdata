//go:build !wowdata_duckdb

package duckdb

import (
	"context"
	"errors"
	"testing"
)

func TestDefaultEngineReportsUnavailable(t *testing.T) {
	engine := NewEngine(":memory:")
	if engine.Available() {
		t.Fatal("Available = true, want false without wowdata_duckdb build tag")
	}
	if err := engine.Open(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Open error = %v, want ErrUnavailable", err)
	}
	if _, err := engine.QueryParquet(context.Background(), "SELECT 1", nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("QueryParquet error = %v, want ErrUnavailable", err)
	}
}
