package sqlitewrite

import (
	"context"
	"sync"
)

var mu sync.Mutex

func Do(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
}
