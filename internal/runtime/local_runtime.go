package runtime

import "sync"

type LocalRuntimeOptions struct {
	CacheRoot string
}

type LocalRuntime struct {
	mu        sync.Mutex
	cacheRoot string
	active    *Context
}

func NewLocalRuntime(opts LocalRuntimeOptions) *LocalRuntime {
	return &LocalRuntime{cacheRoot: opts.CacheRoot}
}

func (r *LocalRuntime) HTTPEnabled() bool {
	return false
}

func (r *LocalRuntime) CacheRoot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cacheRoot
}

func (r *LocalRuntime) SetCacheRoot(value string) {
	r.mu.Lock()
	r.cacheRoot = value
	r.mu.Unlock()
}

func (r *LocalRuntime) SetActiveContext(ctx *Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx == nil {
		r.active = nil
		return
	}
	copy := *ctx
	r.active = &copy
}

func (r *LocalRuntime) ActiveContext() *Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil {
		return nil
	}
	copy := *r.active
	return &copy
}
