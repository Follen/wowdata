package config

import "testing"

func TestDefaultPrepareMatrixHasTwentyThreeTargets(t *testing.T) {
	cfg := Default()
	targets := cfg.Prepare.Targets
	if len(targets) != 23 {
		t.Fatalf("targets = %d, want 23", len(targets))
	}
	assertTarget(t, targets, "CN Retail", "cn", "wow", "zhCN")
	assertTarget(t, targets, "US PTR", "us", "wowt", "enUS")
	assertTarget(t, targets, "EU PTR", "eu", "wowt", "enUS")
	assertTarget(t, targets, "TW Classic Titan", "tw", "wow_classic_titan", "zhTW")
}

func TestDefaultResourceLimitsFitTenGBServer(t *testing.T) {
	limits := Default().Limits
	if limits.MaxParallelContextPrepares != 2 {
		t.Fatalf("context prepares = %d, want 2", limits.MaxParallelContextPrepares)
	}
	if limits.MaxParallelTableMaterializations != 2 {
		t.Fatalf("table materializations = %d, want 2", limits.MaxParallelTableMaterializations)
	}
	if limits.MaxParallelDownloads != 16 || limits.MaxParallelQueries != 16 {
		t.Fatalf("download/query limits = %d/%d, want 16/16", limits.MaxParallelDownloads, limits.MaxParallelQueries)
	}
	if limits.MemorySoftLimitMB != 4096 || limits.MemoryHardLimitMB != 8192 {
		t.Fatalf("memory limits = %d/%d, want 4096/8192", limits.MemorySoftLimitMB, limits.MemoryHardLimitMB)
	}
}

func assertTarget(t *testing.T, targets []PrepareTarget, label, region, product, locale string) {
	t.Helper()
	for _, target := range targets {
		if target.Label == label {
			if target.Region != region || target.Product != product || target.Locale != locale {
				t.Fatalf("%s = %#v, want %s/%s/%s", label, target, region, product, locale)
			}
			return
		}
	}
	t.Fatalf("target %q not found in %#v", label, targets)
}
