package casc

import "testing"

func TestResolveProductAlias(t *testing.T) {
	cases := []struct {
		in        string
		wantProd  string
		wantFlav  string
		wantFound bool
	}{
		{"forever", "wow_classic_beta", "Forever", true},
		{"FOREVER", "wow_classic_beta", "Forever", true},
		{" classic-plus ", "wow_classic_beta", "Forever", true},
		{"wow_classic_beta", "", "", false},
		{"wow", "", "", false},
		{"nonsense", "", "", false},
	}
	for _, c := range cases {
		alias, ok := ResolveProductAlias(c.in)
		if ok != c.wantFound {
			t.Errorf("ResolveProductAlias(%q) found = %v, want %v", c.in, ok, c.wantFound)
			continue
		}
		if !ok {
			continue
		}
		if alias.Product != c.wantProd || alias.Flavor != c.wantFlav {
			t.Errorf("ResolveProductAlias(%q) = %+v, want product=%s flavor=%s", c.in, alias, c.wantProd, c.wantFlav)
		}
	}
}

// The alias names a flavor, not a fixed product, so a build that no longer carries
// that flavor must be rejected instead of answering for another game.
func TestProductAliasRejectsSlotDrift(t *testing.T) {
	alias, ok := ResolveProductAlias("forever")
	if !ok {
		t.Fatal("forever alias is missing")
	}
	if !alias.Matches("wow_classic_beta", "1.60.1.69893") {
		t.Error("forever alias must accept the 1.60.x Forever build")
	}
	if alias.Matches("wow_classic_beta", "5.5.0.62071") {
		t.Error("forever alias must reject the former 5.5.x occupant")
	}
	if alias.Matches("wow_classic_era", "1.60.1.69893") {
		t.Error("forever alias must reject a different product")
	}
	if !(ProductAlias{}).Matches("anything", "any") {
		t.Error("a zero alias must be a no-op")
	}
}

func TestProductAliasesTargetKnownProducts(t *testing.T) {
	known := make(map[string]bool, len(KnownProducts))
	for _, product := range KnownProducts {
		known[product] = true
	}
	names := AliasNames()
	if len(names) == 0 {
		t.Fatal("expected at least one alias")
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("alias names are not sorted: %v", names)
		}
	}
	for _, name := range names {
		alias, _ := ResolveProductAlias(name)
		if !known[alias.Product] {
			t.Errorf("alias %s targets unknown product %s", name, alias.Product)
		}
	}
}
