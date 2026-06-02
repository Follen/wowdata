package service

import (
	"context"
	"errors"
	"testing"
)

func TestUnavailableQueryServiceReturnsCapabilityUnavailable(t *testing.T) {
	svc := UnavailableQueryService{}

	_, err := svc.Tables(context.Background(), TablesRequest{})
	if err == nil {
		t.Fatal("Tables error = nil, want unavailable error")
	}

	var unavailable CapabilityUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("Tables error = %T %[1]v, want CapabilityUnavailableError", err)
	}
	if unavailable.Code != "query_engine_unavailable" {
		t.Fatalf("Code = %q, want query_engine_unavailable", unavailable.Code)
	}
	if unavailable.Capability != "query engine" {
		t.Fatalf("Capability = %q, want query engine", unavailable.Capability)
	}
}
