package app

import (
	"encoding/json"

	"wowdata/internal/db2"
	"wowdata/internal/dbd"

	"github.com/spf13/cobra"
)

type DB2Service struct {
	loadDBDFunc  func(tableName string) (*dbd.Parser, error)
	openTableFunc func(tableName string) (*db2.WDCReader, *db2.DBCReader, error)
}

func NewDB2Handler() func(cmd *cobra.Command, args []string) error {
	svc := &DB2Service{}
	return svc.dispatch
}

func (s *DB2Service) dispatch(cmd *cobra.Command, args []string) error {
	use := cmd.Name()
	switch {
	case containsWord(cmd.CommandPath(), "db2 schema") || containsWord(use, "schema"):
		return s.handleSchema(cmd, args)
	case containsWord(cmd.CommandPath(), "db2 rows") || containsWord(use, "rows"):
		return s.handleRows(cmd, args)
	case containsWord(cmd.CommandPath(), "db2 search") || containsWord(use, "search"):
		return s.handleSearch(cmd, args)
	case containsWord(cmd.CommandPath(), "db2 foreign-key") || containsWord(use, "foreign-key"):
		return s.handleForeignKey(cmd, args)
	case containsWord(cmd.CommandPath(), "db2 stream") || containsWord(use, "stream"):
		return s.handleStream(cmd, args)
	default:
		return s.handleList(cmd, args)
	}
}

func (s *DB2Service) handleSchema(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("db2 schema", "missing_argument", "table name required"))
	}
	// Schema output uses DBD parser when DB data is loaded
	resp := NewSuccessResponse("db2 schema", map[string]interface{}{
		"table": args[0],
		"note":  "dbd schema loading requires warmup context",
	})
	return writeJSON(cmd.OutOrStdout(), resp)
}

func (s *DB2Service) handleRows(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("db2 rows", "missing_argument", "table name required"))
	}

	idFlag, _ := cmd.Flags().GetUint32("id")
	limitFlag, _ := cmd.Flags().GetInt("limit")

	resp := NewSuccessResponse("db2 rows", map[string]interface{}{
		"table":    args[0],
		"id":       idFlag,
		"limit":    limitFlag,
		"note":     "db2 query requires warmup context with data files",
		"rows":     []interface{}{},
	})
	return writeJSON(cmd.OutOrStdout(), resp)
}

func (s *DB2Service) handleSearch(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("db2 search", "missing_argument", "table name required"))
	}

	fieldFlag, _ := cmd.Flags().GetString("field")
	queryFlag, _ := cmd.Flags().GetString("query")

	resp := NewSuccessResponse("db2 search", map[string]interface{}{
		"table":   args[0],
		"field":   fieldFlag,
		"query":   queryFlag,
		"note":    "db2 search requires warmup context",
		"results": []interface{}{},
	})
	return writeJSON(cmd.OutOrStdout(), resp)
}

func (s *DB2Service) handleForeignKey(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("db2 foreign-key", "missing_argument", "table name required"))
	}

	fieldFlag, _ := cmd.Flags().GetString("field")
	valueFlag, _ := cmd.Flags().GetUint32("value")

	resp := NewSuccessResponse("db2 foreign-key", map[string]interface{}{
		"table":   args[0],
		"field":   fieldFlag,
		"value":   valueFlag,
		"note":    "db2 foreign-key requires warmup context",
		"rows":    []interface{}{},
	})
	return writeJSON(cmd.OutOrStdout(), resp)
}

func (s *DB2Service) handleStream(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("db2 stream", "missing_argument", "table name required"))
	}

	limitFlag, _ := cmd.Flags().GetInt("limit")

	// Stream as JSON lines directly
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.Encode(map[string]interface{}{
		"ok":      true,
		"command": "db2 stream",
		"data": map[string]interface{}{
			"table": args[0],
			"limit": limitFlag,
			"note":  "db2 stream requires warmup context",
		},
	})
	return nil
}

func (s *DB2Service) handleList(cmd *cobra.Command, args []string) error {
	resp := NewSuccessResponse("db2", map[string]interface{}{
		"note": "use db2 subcommands: schema, rows, search, foreign-key, stream",
	})
	return writeJSON(cmd.OutOrStdout(), resp)
}

func containsWord(haystack, needle string) bool {
	parts := splitWords(haystack)
	for _, p := range parts {
		if p == needle {
			return true
		}
	}
	return false
}

func splitWords(s string) []string {
	var result []string
	current := ""
	for _, c := range s {
		if c == ' ' || c == '/' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
