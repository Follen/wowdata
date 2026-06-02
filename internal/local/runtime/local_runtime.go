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
	r.active = copyContext(ctx)
}

func (r *LocalRuntime) ActiveContext() *Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	return copyContext(r.active)
}

func copyContext(ctx *Context) *Context {
	if ctx == nil {
		return nil
	}
	copy := *ctx
	if ctx.TablesReady != nil {
		copy.TablesReady = make(map[string]bool, len(ctx.TablesReady))
		for table, ready := range ctx.TablesReady {
			copy.TablesReady[table] = ready
		}
	}
	return &copy
}
