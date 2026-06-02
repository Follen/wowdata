package http

import (
	"container/list"
	"sync"

	appruntime "wowdata/internal/local/runtime"
)

type ContextPool struct {
	mu      sync.Mutex
	max     int
	entries map[string]*contextPoolEntry
	lru     *list.List
	pinned  map[string]struct{}
}

type contextPoolEntry struct {
	key     string
	ctx     *appruntime.Context
	element *list.Element
}

func NewContextPool(max int) *ContextPool {
	if max < 1 {
		max = 1
	}
	return &ContextPool{
		max:     max,
		entries: make(map[string]*contextPoolEntry),
		lru:     list.New(),
		pinned:  make(map[string]struct{}),
	}
}

func (p *ContextPool) Pin(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.pinned[key] = struct{}{}
}

func (p *ContextPool) Put(key string, ctx *appruntime.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.entries[key]; ok {
		entry.ctx = ctx
		p.lru.MoveToFront(entry.element)
		return
	}

	entry := &contextPoolEntry{key: key, ctx: ctx}
	entry.element = p.lru.PushFront(entry)
	p.entries[key] = entry
	p.evict()
}

func (p *ContextPool) Get(key string) (*appruntime.Context, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.entries[key]
	if !ok {
		return nil, false
	}
	p.lru.MoveToFront(entry.element)
	return entry.ctx, true
}

func (p *ContextPool) evict() {
	for len(p.entries) > p.max {
		var victim *list.Element
		for element := p.lru.Back(); element != nil; element = element.Prev() {
			entry := element.Value.(*contextPoolEntry)
			if _, ok := p.pinned[entry.key]; !ok {
				victim = element
				break
			}
		}
		if victim == nil {
			return
		}
		entry := victim.Value.(*contextPoolEntry)
		p.lru.Remove(victim)
		delete(p.entries, entry.key)
	}
}
