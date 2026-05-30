package golden

import "path/filepath"

type CompareResult struct {
	Fixture string `json:"fixture"`
	Equal   bool   `json:"equal"`
	Reason  string `json:"reason,omitempty"`
}

func FixturePath(group string, name string) string {
	return filepath.ToSlash(filepath.Join("fixtures", "golden", group, name+".json"))
}
