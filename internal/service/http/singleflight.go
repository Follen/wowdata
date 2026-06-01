package http

import "sync"

type Singleflight struct {
	mu       sync.Mutex
	inflight map[string]*singleflightCall
}

type singleflightCall struct {
	done  chan struct{}
	value interface{}
	err   error
}

func NewSingleflight() *Singleflight {
	return &Singleflight{
		inflight: make(map[string]*singleflightCall),
	}
}

func (g *Singleflight) Do(key string, fn func() (interface{}, error)) (interface{}, error) {
	g.mu.Lock()
	if call, ok := g.inflight[key]; ok {
		g.mu.Unlock()
		<-call.done
		return call.value, call.err
	}
	call := &singleflightCall{done: make(chan struct{})}
	g.inflight[key] = call
	g.mu.Unlock()

	call.value, call.err = fn()

	g.mu.Lock()
	delete(g.inflight, key)
	g.mu.Unlock()
	close(call.done)

	return call.value, call.err
}
