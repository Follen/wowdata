package http

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"wowdata/internal/cache"
	"wowdata/internal/cache/metadata"
)

type PruneBuild struct {
	Region   string
	Product  string
	BuildKey string
	Path     string
	Bytes    int64
}

type PruneAudit struct {
	Actor  string
	Path   string
	Bytes  int64
	Reason string
}

type PruneState struct {
	Active   map[string]struct{}
	Pinned   map[string]struct{}
	InFlight map[string]struct{}
}

type PrunePlan struct {
	Delete []PruneBuild
}

type PruneStore interface {
	PruneCandidates(ctx context.Context) ([]PruneBuild, error)
	PruneState(ctx context.Context) (PruneState, error)
	RecordPruneAudit(ctx context.Context, audit PruneAudit) error
}

type Pruner struct {
	Store PruneStore
	Actor string
}

type MetadataPruneStore struct {
	DB        *sql.DB
	CacheRoot string
}

func (s PruneState) CanDelete(build PruneBuild) bool {
	key := pruneBuildKey(build.Region, build.Product, build.BuildKey)
	if _, ok := s.Active[key]; ok {
		return false
	}
	if _, ok := s.Pinned[key]; ok {
		return false
	}
	if _, ok := s.InFlight[key]; ok {
		return false
	}
	return true
}

func (p Pruner) CanDelete(build PruneBuild, state PruneState) bool {
	return state.CanDelete(build)
}

func (p Pruner) Plan(ctx context.Context) (PrunePlan, error) {
	if p.Store == nil {
		return PrunePlan{}, fmt.Errorf("prune store is required")
	}
	state, err := p.Store.PruneState(ctx)
	if err != nil {
		return PrunePlan{}, err
	}
	candidates, err := p.Store.PruneCandidates(ctx)
	if err != nil {
		return PrunePlan{}, err
	}
	actor := p.Actor
	if actor == "" {
		actor = "http-pruner"
	}
	plan := PrunePlan{Delete: make([]PruneBuild, 0, len(candidates))}
	for _, candidate := range candidates {
		if !state.CanDelete(candidate) {
			continue
		}
		plan.Delete = append(plan.Delete, candidate)
		if err := p.Store.RecordPruneAudit(ctx, PruneAudit{
			Actor:  actor,
			Path:   candidate.Path,
			Bytes:  candidate.Bytes,
			Reason: "prune_plan",
		}); err != nil {
			return PrunePlan{}, err
		}
	}
	return plan, nil
}

func (s MetadataPruneStore) PruneCandidates(ctx context.Context) ([]PruneBuild, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("metadata db is required")
	}
	rows, err := s.DB.QueryContext(ctx, `
SELECT region, product, build_key
FROM builds
ORDER BY discovered_at ASC, build_key ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var builds []PruneBuild
	for rows.Next() {
		var build PruneBuild
		if err := rows.Scan(&build.Region, &build.Product, &build.BuildKey); err != nil {
			return nil, err
		}
		build.Path = cache.RawCASCPath(s.CacheRoot, build.Region, build.Product, build.BuildKey)
		if info, err := os.Stat(build.Path); err == nil {
			build.Bytes = info.Size()
		}
		builds = append(builds, build)
	}
	return builds, rows.Err()
}

func (s MetadataPruneStore) PruneState(ctx context.Context) (PruneState, error) {
	if s.DB == nil {
		return PruneState{}, fmt.Errorf("metadata db is required")
	}
	rows, err := s.DB.QueryContext(ctx, `
SELECT region, product, build_key
FROM builds
WHERE active = 1`)
	if err != nil {
		return PruneState{}, err
	}
	defer rows.Close()

	state := PruneState{
		Active:   make(map[string]struct{}),
		Pinned:   make(map[string]struct{}),
		InFlight: make(map[string]struct{}),
	}
	for rows.Next() {
		var region, product, buildKey string
		if err := rows.Scan(&region, &product, &buildKey); err != nil {
			return PruneState{}, err
		}
		state.Active[pruneBuildKey(region, product, buildKey)] = struct{}{}
	}
	return state, rows.Err()
}

func (s MetadataPruneStore) RecordPruneAudit(_ context.Context, audit PruneAudit) error {
	if s.DB == nil {
		return fmt.Errorf("metadata db is required")
	}
	return metadata.RecordCacheAudit(s.DB, metadata.CacheAudit{
		Actor:  audit.Actor,
		Path:   audit.Path,
		Bytes:  audit.Bytes,
		Reason: audit.Reason,
	})
}

func pruneBuildKey(region, product, buildKey string) string {
	return region + "/" + product + "/" + buildKey
}
