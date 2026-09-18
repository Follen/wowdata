package casc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadSkillAliasTables reads the friendly-name table shipped with the Skill. The file
// is the user-facing documentation of the same names the CLI resolves, so the two must
// not drift. It is parsed by hand to keep this package dependency-free.
func loadSkillAliasTables(t *testing.T) (clients map[string]string, flavors map[string]string) {
	t.Helper()
	path := filepath.Join("..", "..", "skill", "wowdata", "references", "clients.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	clients = map[string]string{}
	flavors = map[string]string{}
	section := ""
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line == trimmed {
			switch strings.TrimSuffix(trimmed, ":") {
			case "clients", "flavorAliases":
				section = strings.TrimSuffix(trimmed, ":")
			default:
				section = ""
			}
			continue
		}
		if section == "" {
			continue
		}
		key, value, found := strings.Cut(trimmed, ":")
		if !found {
			continue
		}
		key = strings.ToLower(strings.Trim(strings.TrimSpace(key), `"`))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if section == "clients" {
			clients[key] = value
		} else {
			flavors[key] = value
		}
	}
	return clients, flavors
}

// The Skill table and the CLI table are written independently, so assert they agree in
// both directions: a documented name the CLI rejects, or a CLI name the Skill omits,
// is a defect.
func TestSkillClientTableMatchesCLIAliases(t *testing.T) {
	clients, flavors := loadSkillAliasTables(t)
	documented := map[string]string{}
	for name, product := range clients {
		documented[name] = product
	}
	for name, product := range flavors {
		if _, duplicate := documented[name]; duplicate {
			t.Errorf("%s is listed as both a client and a flavor alias", name)
		}
		documented[name] = product
	}
	if len(documented) == 0 {
		t.Fatal("no aliases parsed from the Skill table")
	}

	for name, product := range documented {
		alias, ok := ResolveProductAlias(name)
		if !ok {
			t.Errorf("Skill documents %q but the CLI does not resolve it", name)
			continue
		}
		if alias.Product != product {
			t.Errorf("Skill maps %q to %s but the CLI maps it to %s", name, product, alias.Product)
		}
	}
	for _, name := range AliasNames() {
		if _, ok := documented[strings.ToLower(name)]; !ok {
			t.Errorf("CLI alias %q is missing from the Skill table", name)
		}
	}

	for name := range clients {
		if alias, _ := ResolveProductAlias(name); alias.Flavor != "" {
			t.Errorf("%q labels a slot but declares flavor %q", name, alias.Flavor)
		}
	}
	for name := range flavors {
		if alias, _ := ResolveProductAlias(name); alias.Flavor == "" {
			t.Errorf("%q names a game but declares no flavor", name)
		}
	}
}
