package http

import "context"

type Materializer interface {
	EnsureTable(ctx context.Context, rc RequestContext, table string) error
}
