package http

import "context"

type PrepareScheduler struct {
	sem chan struct{}
}

func NewPrepareScheduler(max int) *PrepareScheduler {
	if max < 1 {
		max = 1
	}
	return &PrepareScheduler{sem: make(chan struct{}, max)}
}

func (s *PrepareScheduler) Run(ctx context.Context, fn func(context.Context) error) error {
	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() {
		<-s.sem
	}()

	return fn(ctx)
}
