package casc

import (
	"strings"
	"testing"
)

func TestKnownProductTitlesCoverDiscovery(t *testing.T) {
	for _, product := range KnownProducts {
		if knownProductTitle(product) == "" {
			t.Errorf("missing fallback title for %s", product)
		}
	}
	if title := knownProductTitle("unknown_product"); title != "" {
		t.Errorf("unknown product title = %q, want empty", title)
	}
}

// The base title must stay version-neutral. wow_classic_beta is a reusable slot that
// has held different releases, so naming a flavor here would mislabel older builds.
func TestProductTitleStaysVersionNeutral(t *testing.T) {
	title := knownProductTitle("wow_classic_beta")
	if strings.Contains(title, "Forever") {
		t.Errorf("base title must not name a flavor: %q", title)
	}
	if !strings.Contains(title, "Classic") {
		t.Errorf("base title must stay recognizable: %q", title)
	}
}

func TestClassicBetaFlavorFollowsVersion(t *testing.T) {
	cases := []struct {
		product string
		version string
		want    string
	}{
		{"wow_classic_beta", "1.60.1.69893", "Forever"},
		{"wow_classic_beta", "5.5.0.62071", ""}, // this slot held the MoP Classic beta
		{"wow_classic_beta", "", ""},
		{"wow_classic_era", "1.15.9.69722", ""}, // the flavor is slot-specific
		{"wow", "12.1.0.69814", ""},
	}
	for _, c := range cases {
		if got := classicBetaFlavor(c.product, c.version); got != c.want {
			t.Errorf("classicBetaFlavor(%q, %q) = %q, want %q", c.product, c.version, got, c.want)
		}
	}
}

func TestProductTitleLabelsForeverOnlyForForeverBuilds(t *testing.T) {
	forever := productTitleWithFlavor("wow_classic_beta", "1.60.1.69893")
	if !strings.Contains(forever, "Classic") || !strings.Contains(forever, "Forever") {
		t.Errorf("1.60.1 build must read as Classic and Forever: %q", forever)
	}
	older := productTitleWithFlavor("wow_classic_beta", "5.5.0.62071")
	if strings.Contains(older, "Forever") {
		t.Errorf("5.5.0 build must not be labelled Forever: %q", older)
	}
	if unknown := productTitleWithFlavor("custom_product", "1.0.0"); unknown != "custom_product" {
		t.Errorf("unknown product title = %q, want the product id", unknown)
	}
}
