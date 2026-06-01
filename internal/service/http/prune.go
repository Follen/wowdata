package http

import (
	"context"
	"fmt"
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

func pruneBuildKey(region, product, buildKey string) string {
	return region + "/" + product + "/" + buildKey
}
