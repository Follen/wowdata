package http

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"wowdata/internal/cache/duckdb"
	"wowdata/internal/config"
)

type RequestContext struct {
	Region  string
	Product string
	Locale  string
}

type ToolPolicy struct {
	ExposeWarmup     bool
	ExposeAdminTools bool
	ExposeDebugTools bool
}

type Service struct {
	cfg          config.HTTPConfig
	materializer Materializer
	flights      *Singleflight

	mu              sync.Mutex
	ensuredTables   map[string]struct{}
	materializeFunc func(context.Context, RequestContext, string) error
}

func NewService(cfg config.HTTPConfig, materializer Materializer) *Service {
	return &Service{
		cfg:             cfg,
		materializer:    materializer,
		flights:         NewSingleflight(),
		ensuredTables:   make(map[string]struct{}),
		materializeFunc: nil,
	}
}

func (s *Service) ResolveRequestContext(rc RequestContext) RequestContext {
	if rc.Region == "" {
		rc.Region = s.cfg.Defaults.Region
	}
	if rc.Product == "" {
		rc.Product = s.cfg.Defaults.Product
	}
	if rc.Locale == "" {
		rc.Locale = s.cfg.Defaults.Locale
	}
	return rc
}

func (s *Service) ToolPolicy() ToolPolicy {
	return ToolPolicy{
		ExposeWarmup:     false,
		ExposeAdminTools: s.cfg.Tools.ExposeAdminTools,
		ExposeDebugTools: s.cfg.Tools.ExposeDebugTools,
	}
}

func (s *Service) Builds() BuildCatalog {
	pinned := make([]PinnedContext, 0, len(s.cfg.Contexts.Pinned))
	for _, ctx := range s.cfg.Contexts.Pinned {
		pinned = append(pinned, PinnedContext{
			Region:  ctx.Region,
			Product: ctx.Product,
			Locale:  ctx.Locale,
			Label:   ctx.Label,
		})
	}
	return BuildCatalog{
		Default: ContextDefaults{
			Region:  s.cfg.Defaults.Region,
			Product: s.cfg.Defaults.Product,
			Locale:  s.cfg.Defaults.Locale,
		},
		Pinned: pinned,
	}
}

func (s *Service) EnsureContext(context.Context, RequestContext) error {
	return nil
}

func (s *Service) RequireCapability(ctx context.Context, rc RequestContext, capability string) error {
	return NewCapabilityError(capabilityErrorCode(capability), capability)
}

func (s *Service) Status() Status {
	return Status{
		OK: true,
		Cache: CacheStatus{
			Root: s.cfg.Cache.Root,
		},
		Memory: MemoryStatus{
			MaxContexts: s.cfg.Contexts.MaxContexts,
		},
	}
}

func (s *Service) EnsureTable(ctx context.Context, rc RequestContext, table string) error {
	table = strings.TrimSpace(table)
	if table == "" {
		return fmt.Errorf("table is required")
	}
	rc = s.ResolveRequestContext(rc)
	key := tableKey(rc, table)

	s.mu.Lock()
	if _, ok := s.ensuredTables[key]; ok {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	_, err := s.flights.Do(key, func() (interface{}, error) {
		s.mu.Lock()
		if _, ok := s.ensuredTables[key]; ok {
			s.mu.Unlock()
			return nil, nil
		}
		s.mu.Unlock()

		if err := s.materialize(ctx, rc, table); err != nil {
			if errors.Is(err, duckdb.ErrUnavailable) {
				return nil, NewCapabilityError("query_engine_unavailable", "query engine")
			}
			return nil, err
		}

		s.mu.Lock()
		s.ensuredTables[key] = struct{}{}
		s.mu.Unlock()
		return nil, nil
	})
	return err
}

func (s *Service) SetMaterializeFuncForTest(fn func(context.Context, RequestContext, string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.materializeFunc = fn
}

func (s *Service) materialize(ctx context.Context, rc RequestContext, table string) error {
	s.mu.Lock()
	fn := s.materializeFunc
	s.mu.Unlock()
	if fn != nil {
		return fn(ctx, rc, table)
	}
	if s.materializer != nil {
		return s.materializer.EnsureTable(ctx, rc, table)
	}
	return NewCapabilityError("query_engine_unavailable", "query engine")
}

func tableKey(rc RequestContext, table string) string {
	return rc.Region + "/" + rc.Product + "/" + rc.Locale + "/" + table
}

func capabilityErrorCode(capability string) string {
	if strings.Contains(capability, "export") {
		return "export_engine_unavailable"
	}
	return "query_engine_unavailable"
}
