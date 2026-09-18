package main

import (
	"testing"

	"wowdata/internal/casc"
)

// The hints are what the CLI offers when a caller has not chosen a product yet, so they
// must list every product the CLI can actually resolve. Naming a flavor here would go
// stale, so labels stay factual and the Skill explains what the slots currently carry.
func TestWarmupProductHintsCoverKnownProducts(t *testing.T) {
	hints := warmupPrompt(nil)["productHints"].([]map[string]interface{})
	known := make(map[string]bool)
	for _, product := range casc.KnownProducts {
		known[product] = true
	}
	seen := make(map[string]bool)
	for _, hint := range hints {
		product, _ := hint["product"].(string)
		if !known[product] {
			t.Errorf("product hint %q is not a known product", product)
			continue
		}
		if seen[product] {
			t.Errorf("duplicate product hint: %s", product)
		}
		seen[product] = true
		if label, _ := hint["label"].(string); label == "" {
			t.Errorf("empty label for %s", product)
		}
	}
	for product := range known {
		if !seen[product] {
			t.Errorf("missing product hint: %s", product)
		}
	}
}
