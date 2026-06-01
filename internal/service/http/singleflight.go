package http

import "sync"

type Singleflight struct {
	mu       sync.Mutex
	inflight map[string]*singleflightCall
}

type singleflightCall struct {
	done       chan struct{}
	value      interface{}
	err        error
	panicValue interface{}
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
		if call.panicValue != nil {
			panic(call.panicValue)
		}
		return call.value, call.err
	}
	call := &singleflightCall{done: make(chan struct{})}
	g.inflight[key] = call
	g.mu.Unlock()

	defer func() {
		close(call.done)
		g.mu.Lock()
		delete(g.inflight, key)
		g.mu.Unlock()
		if call.panicValue != nil {
			panic(call.panicValue)
		}
	}()

	defer func() {
		if value := recover(); value != nil {
			call.panicValue = value
		}
	}()

	call.value, call.err = fn()

	return call.value, call.err
}
