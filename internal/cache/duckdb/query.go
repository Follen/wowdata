package duckdb

import (
	"fmt"
	"regexp"
)

var safeIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func IsSafeIdentifier(value string) bool {
	return safeIdentifierPattern.MatchString(value)
}

func SelectByID(table string, field string, id uint32) (string, []interface{}, error) {
	if !IsSafeIdentifier(table) {
		return "", nil, fmt.Errorf("unsafe table identifier %q", table)
	}
	if !IsSafeIdentifier(field) {
		return "", nil, fmt.Errorf("unsafe field identifier %q", field)
	}
	return fmt.Sprintf(`SELECT * FROM "%s" WHERE "%s" = ?`, table, field), []interface{}{id}, nil
}
