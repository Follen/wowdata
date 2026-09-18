package casc

import (
	"sort"
	"strings"
)

// ProductAlias is a friendly query name for a CDN product slot. Slots such as
// wow_classic_beta are reused between releases, so the flavor the caller expects
// travels with the alias and is verified against the build that actually resolves.
type ProductAlias struct {
	Name    string
	Product string
	Flavor  string
}

var productAliases = map[string]ProductAlias{
	"forever":      {Name: "forever", Product: "wow_classic_beta", Flavor: "Forever"},
	"classic-plus": {Name: "classic-plus", Product: "wow_classic_beta", Flavor: "Forever"},
}

// ResolveProductAlias maps a friendly name onto the CDN product carrying it.
// Unknown names report false so real product IDs and typos pass through untouched.
func ResolveProductAlias(name string) (ProductAlias, bool) {
	alias, ok := productAliases[strings.ToLower(strings.TrimSpace(name))]
	return alias, ok
}

// Matches reports whether a resolved build still carries the flavor this alias
// promised. A slot that moved on to another release fails the check rather than
// silently answering for the wrong game.
func (a ProductAlias) Matches(product, version string) bool {
	if a.Name == "" {
		return true
	}
	return a.Product == product && classicBetaFlavor(product, version) == a.Flavor
}

// AliasNames lists the supported aliases in stable order.
func AliasNames() []string {
	names := make([]string, 0, len(productAliases))
	for name := range productAliases {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
