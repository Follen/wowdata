package http

import (
	"context"
	"errors"
)

var ErrQueryEngineUnavailable = errors.New("query_engine_unavailable")

type Materializer interface {
	EnsureTable(ctx context.Context, rc RequestContext, table string) error
}

func NewQueryEngineUnavailableError(message string) error {
	if message == "" {
		message = "query engine unavailable"
	}
	return queryEngineUnavailableError{CapabilityError: CapabilityError{
		Code:    "query_engine_unavailable",
		Message: message,
	}}
}

type queryEngineUnavailableError struct {
	CapabilityError
}

func (e queryEngineUnavailableError) Is(target error) bool {
	return target == ErrQueryEngineUnavailable
}
