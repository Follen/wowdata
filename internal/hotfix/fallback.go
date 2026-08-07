package hotfix

import (
	"context"
	"time"
)

type SnapshotSource struct {
	Reader     *Reader
	Product    string
	Build      string
	Region     uint32
	Locale     string
	CapturedAt time.Time
	MaxAge     time.Duration
}

func (s SnapshotSource) Query(ctx context.Context, q Query) (Result, error) {
	if s.Reader == nil {
		return Result{}, errf("hotfix_unavailable", "snapshot", "snapshot reader is nil")
	}
	if s.Product != "" && q.Product != s.Product {
		return Result{}, errf("hotfix_build_unavailable", "product", "snapshot product mismatch")
	}
	if s.Build != "" && !buildMatches(q.Build, s.Build) {
		return Result{}, errf("hotfix_build_mismatch", "build", "snapshot build %s does not match %s", s.Build, q.Build)
	}
	if s.Region != 0 && q.Region != s.Region {
		return Result{}, errf("hotfix_build_unavailable", "region", "snapshot region mismatch")
	}
	if s.Locale != "" && q.Locale != s.Locale {
		return Result{}, errf("hotfix_locale_mismatch", "locale", "snapshot locale mismatch")
	}
	if s.MaxAge > 0 && !s.CapturedAt.IsZero() && time.Since(s.CapturedAt) > s.MaxAge {
		return Result{}, errf("hotfix_coverage_incomplete", "snapshot", "snapshot is older than %s", s.MaxAge)
	}
	r, e := s.Reader.QueryEntries(q)
	if e != nil {
		return Result{}, e
	}
	r.Source = "raidbots"
	r.Coverage.Source = "raidbots"
	r.Coverage.Complete = false
	r.Coverage.CapturedAt = s.CapturedAt
	return r, nil
}

type FallbackSource struct {
	Primary Source
	Recent  Source
}

func (s FallbackSource) Query(ctx context.Context, q Query) (Result, error) {
	if s.Primary == nil {
		return Result{}, errf("hotfix_unavailable", "primary", "primary source is nil")
	}
	r, e := s.Primary.Query(ctx, q)
	if e == nil {
		return r, nil
	}
	if s.Recent == nil || (!q.Latest && q.Page > 1) {
		return Result{}, errf("hotfix_coverage_incomplete", "fallback", "%v", e)
	}
	rr, re := s.Recent.Query(ctx, q)
	if re != nil {
		return Result{}, e
	}
	rr.Warnings = append(rr.Warnings, "wago_unavailable_fell_back_to_raidbots_snapshot")
	rr.Source = "raidbots"
	return rr, nil
}
