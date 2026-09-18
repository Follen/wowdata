package casc

import (
	"sort"
	"strings"
)

// ProductAlias is a friendly query name for a CDN product. Slots such as
// wow_classic_beta are reused between releases, so an alias that names a flavor also
// carries the flavor it expects; that expectation is verified against the build that
// actually resolves.
type ProductAlias struct {
	Name    string
	Product string
	// Flavor is the version-derived flavor the alias promises, or "" when the alias
	// simply names a slot and makes no claim about what currently occupies it.
	Flavor string
}

// productAliases holds every friendly name the CLI accepts. Keys are lowercased
// because lookup normalizes the caller's input. No key may equal a real product ID,
// which TestProductAliasesDoNotShadowProducts enforces.
var productAliases = map[string]ProductAlias{
	// Retail line.
	"retail": {Name: "retail", Product: "wow"},

	// Retail test and beta slots.
	"ptr":     {Name: "ptr", Product: "wowt"},
	"正式服测试服":  {Name: "正式服测试服", Product: "wowt"},
	"正式服 ptr": {Name: "正式服 PTR", Product: "wowt"},
	"ptr2":    {Name: "ptr2", Product: "wowxptr"},
	"beta":    {Name: "beta", Product: "wow_beta"},

	// Classic line.
	"classic":             {Name: "classic", Product: "wow_classic"},
	"怀旧服":                 {Name: "怀旧服", Product: "wow_classic"},
	"classic-ptr":         {Name: "classic-ptr", Product: "wow_classic_ptr"},
	"怀旧服 ptr":             {Name: "怀旧服 PTR", Product: "wow_classic_ptr"},
	"怀旧服测试服":              {Name: "怀旧服测试服", Product: "wow_classic_ptr"},
	"classic-era":         {Name: "classic-era", Product: "wow_classic_era"},
	"怀中怀":                 {Name: "怀中怀", Product: "wow_classic_era"},
	"60级":                 {Name: "60级", Product: "wow_classic_era"},
	"香草服":                 {Name: "香草服", Product: "wow_classic_era"},
	"classic-era-ptr":     {Name: "classic-era-ptr", Product: "wow_classic_era_ptr"},
	"classic-anniversary": {Name: "classic-anniversary", Product: "wow_anniversary"},
	"classic-titan":       {Name: "classic-titan", Product: "wow_classic_titan"},
	"泰坦服":                 {Name: "泰坦服", Product: "wow_classic_titan"},
	"泰坦重铸":                {Name: "泰坦重铸", Product: "wow_classic_titan"},
	"时光服":                 {Name: "时光服", Product: "wow_classic_titan"},

	// classic-beta names the slot itself, so it claims no particular occupant.
	"classic-beta": {Name: "classic-beta", Product: "wow_classic_beta"},

	// forever and classic-plus name a game, not a slot, so they pin the flavor.
	"forever":      {Name: "forever", Product: "wow_classic_beta", Flavor: "Forever"},
	"classic-plus": {Name: "classic-plus", Product: "wow_classic_beta", Flavor: "Forever"},
}

// ResolveProductAlias maps a friendly name onto the CDN product carrying it.
// Unknown names report false so real product IDs and typos pass through untouched.
func ResolveProductAlias(name string) (ProductAlias, bool) {
	alias, ok := productAliases[strings.ToLower(strings.TrimSpace(name))]
	return alias, ok
}

// Matches reports whether a resolved build still satisfies this alias. An alias that
// names a flavor fails once the slot moves on, rather than silently answering for a
// different game; an alias that only names a slot accepts whatever it holds.
func (a ProductAlias) Matches(product, version string) bool {
	if a.Name == "" {
		return true
	}
	if a.Product != product {
		return false
	}
	if a.Flavor == "" {
		return true
	}
	return classicBetaFlavor(product, version) == a.Flavor
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
