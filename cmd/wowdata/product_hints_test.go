package main

import (
	"strings"
	"testing"

	"wowdata/internal/casc"
)

func TestWarmupProductHintsCoverKnownProducts(t *testing.T) {
	hints := warmupPrompt(nil)["productHints"].([]map[string]interface{})
	known := make(map[string]bool)
	for _, product := range casc.KnownProducts {
		known[product] = true
	}
	seen := make(map[string]bool)
	for _, hint := range hints {
		product := hint["product"].(string)
		if !known[product] || seen[product] {
			t.Errorf("unexpected or duplicate product hint: %s", product)
		}
		seen[product] = true
		label, _ := hint["label"].(string)
		if label == "" {
			t.Errorf("empty label for %s", product)
		}
		if product == "wow_classic_beta" && !strings.Contains(label, "Forever") {
			t.Errorf("Classic Beta hint must mention Forever: %q", label)
		}
	}
	for product := range known {
		if !seen[product] {
			t.Errorf("missing product hint: %s", product)
		}
	}
}
