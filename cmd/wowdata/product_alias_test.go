package main

import (
	"testing"

	"wowdata/internal/app"
	"wowdata/internal/casc"
)

func TestResolveTargetResolvesForeverAlias(t *testing.T) {
	layout := mustTestLayout(t)
	cmd := app.NewRootCommandWithService(nil)
	for name, value := range map[string]string{"source": "remote", "region": "us", "product": "forever", "build": "latest", "locale": "zhCN"} {
		if err := cmd.PersistentFlags().Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	resolved, err := resolveTarget(cmd, layout)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Target.Product != "wow_classic_beta" {
		t.Fatalf("product = %q, want wow_classic_beta", resolved.Target.Product)
	}
	if resolved.Alias.Name != "forever" || resolved.Alias.Flavor != "Forever" {
		t.Fatalf("alias = %+v, want the forever alias", resolved.Alias)
	}
}

func TestResolveTargetLeavesRealProductUntouched(t *testing.T) {
	layout := mustTestLayout(t)
	cmd := app.NewRootCommandWithService(nil)
	for name, value := range map[string]string{"source": "remote", "region": "us", "product": "wow_classic_era", "build": "latest", "locale": "zhCN"} {
		if err := cmd.PersistentFlags().Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	resolved, err := resolveTarget(cmd, layout)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Target.Product != "wow_classic_era" {
		t.Fatalf("product = %q, want wow_classic_era", resolved.Target.Product)
	}
	if resolved.Alias.Name != "" {
		t.Fatalf("alias = %+v, want none for a real product id", resolved.Alias)
	}
}

func TestVerifyProductAlias(t *testing.T) {
	alias, ok := casc.ResolveProductAlias("forever")
	if !ok {
		t.Fatal("forever alias is missing")
	}
	forever := casc.VersionEntry{Product: "wow_classic_beta", VersionsName: "1.60.1.69893"}
	if err := verifyProductAlias(alias, "wow_classic_beta", forever); err != nil {
		t.Errorf("Forever build must pass: %v", err)
	}
	drifted := casc.VersionEntry{Product: "wow_classic_beta", VersionsName: "5.5.0.62071"}
	if err := verifyProductAlias(alias, "wow_classic_beta", drifted); err == nil {
		t.Error("a build that no longer carries Forever must be rejected")
	}
	if err := verifyProductAlias(casc.ProductAlias{}, "wow", forever); err != nil {
		t.Errorf("a zero alias must be a no-op: %v", err)
	}
}
