package http

import (
	"context"
	"errors"
	"strings"
	"testing"

	"wowdata/internal/config"
)

func TestServiceDefaultsToCNRetail(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)

	rc := svc.ResolveRequestContext(RequestContext{})

	if rc.Region != "cn" || rc.Product != "wow" || rc.Locale != "zhCN" {
		t.Fatalf("context = %#v", rc)
	}
}

func TestServiceRequestContextOverridesDefaults(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)

	rc := svc.ResolveRequestContext(RequestContext{
		Region:  "us",
		Product: "wow_classic",
		Locale:  "enUS",
	})

	if rc.Region != "us" || rc.Product != "wow_classic" || rc.Locale != "enUS" {
		t.Fatalf("context = %#v", rc)
	}
}

func TestServiceToolPolicyGatesWarmupAndAdminTools(t *testing.T) {
	cfg := config.DefaultHTTPConfig()
	cfg.Tools.ExposeAdminTools = false
	svc := NewService(cfg, nil)
	policy := svc.ToolPolicy()
	if policy.ExposeWarmup {
		t.Fatal("HTTP ordinary tools must not expose wow_warmup")
	}
	if policy.ExposeAdminTools {
		t.Fatal("admin tools should be hidden when disabled")
	}

	cfg.Tools.ExposeAdminTools = true
	svc = NewService(cfg, nil)
	policy = svc.ToolPolicy()
	if !policy.ExposeAdminTools {
		t.Fatal("admin tools should be exposed when enabled")
	}
}

func TestServiceBuildsReportsDefaultsAndPinnedContexts(t *testing.T) {
	cfg := config.DefaultHTTPConfig()
	cfg.Defaults.Region = "eu"
	cfg.Defaults.Product = "wowt"
	cfg.Defaults.Locale = "enUS"
	cfg.Contexts.Pinned = []config.HTTPPinnedContext{
		{Region: "cn", Product: "wow", Locale: "zhCN", Label: "CN Retail"},
	}
	svc := NewService(cfg, nil)

	builds := svc.Builds()

	if builds.Default.Region != "eu" || builds.Default.Product != "wowt" || builds.Default.Locale != "enUS" {
		t.Fatalf("default context = %#v", builds.Default)
	}
	if len(builds.Pinned) != 1 || builds.Pinned[0].Label != "CN Retail" {
		t.Fatalf("pinned contexts = %#v", builds.Pinned)
	}
}

func TestServiceEnsureTableCachesSuccessfulMaterialization(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)
	var calls int
	svc.SetMaterializeFuncForTest(func(context.Context, RequestContext, string) error {
		calls++
		return nil
	})

	rc := RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"}
	if err := svc.EnsureTable(context.Background(), rc, "SpellName"); err != nil {
		t.Fatalf("EnsureTable first: %v", err)
	}
	if err := svc.EnsureTable(context.Background(), rc, "SpellName"); err != nil {
		t.Fatalf("EnsureTable second: %v", err)
	}

	if calls != 1 {
		t.Fatalf("materialize calls = %d, want 1", calls)
	}
}

func TestServiceEnsureTableRejectsEmptyTable(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)

	if err := svc.EnsureTable(context.Background(), RequestContext{}, ""); err == nil {
		t.Fatal("EnsureTable empty table error = nil")
	}
}

func TestServiceEnsureTableRejectsMissingMaterializerWithoutCaching(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)
	rc := RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"}

	err := svc.EnsureTable(context.Background(), rc, "SpellName")
	if err == nil {
		t.Fatal("EnsureTable missing materializer error = nil")
	}
	if !strings.Contains(err.Error(), "materializer") {
		t.Fatalf("EnsureTable error = %q, want materializer context", err.Error())
	}

	err = svc.EnsureTable(context.Background(), rc, "SpellName")
	if err == nil {
		t.Fatal("EnsureTable second missing materializer error = nil")
	}
	if !strings.Contains(err.Error(), "materializer") {
		t.Fatalf("EnsureTable second error = %q, want materializer context", err.Error())
	}
}

func TestServiceEnsureTableRetriesAfterMaterializationError(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)
	var calls int
	fail := errors.New("materialize failed")
	svc.SetMaterializeFuncForTest(func(context.Context, RequestContext, string) error {
		calls++
		if calls == 1 {
			return fail
		}
		return nil
	})

	rc := RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"}
	if err := svc.EnsureTable(context.Background(), rc, "SpellName"); !errors.Is(err, fail) {
		t.Fatalf("EnsureTable first error = %v, want %v", err, fail)
	}
	if err := svc.EnsureTable(context.Background(), rc, "SpellName"); err != nil {
		t.Fatalf("EnsureTable second: %v", err)
	}

	if calls != 2 {
		t.Fatalf("materialize calls = %d, want 2", calls)
	}
}

func TestServiceStatusReportsCacheAndContextLimits(t *testing.T) {
	cfg := config.DefaultHTTPConfig()
	cfg.Cache.Root = "test-cache"
	cfg.Contexts.MaxContexts = 7
	svc := NewService(cfg, nil)

	status := svc.Status()

	if !status.OK {
		t.Fatal("status OK = false, want true")
	}
	if status.Cache.Root != "test-cache" {
		t.Fatalf("cache root = %q, want test-cache", status.Cache.Root)
	}
	if status.Memory.MaxContexts != 7 {
		t.Fatalf("max contexts = %d, want 7", status.Memory.MaxContexts)
	}
}
