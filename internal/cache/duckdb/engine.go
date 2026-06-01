package duckdb

import "unicode"

type Engine struct{}

func IsSafeIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if i == 0 && !(r == '_' || unicode.IsLetter(r)) {
			return false
		}
		if !(r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}
