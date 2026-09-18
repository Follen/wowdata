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
	if title := knownProductTitle("wow_classic_beta"); !strings.Contains(title, "Classic") || !strings.Contains(title, "Forever") {
		t.Errorf("beta title must retain Classic and mention Forever: %q", title)
	}
	if title := knownProductTitle("unknown_product"); title != "" {
		t.Errorf("unknown product title = %q, want empty", title)
	}
}
