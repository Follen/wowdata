package app

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	appruntime "wowdata/internal/local/runtime"

	"github.com/spf13/cobra"
)

type DB2Service struct {
	store appruntime.DB2Store
}

func NewDB2Handler() func(cmd *cobra.Command, args []string) error {
	return NewDB2HandlerWithStore(nil)
}

func NewDB2HandlerWithStore(store appruntime.DB2Store) func(cmd *cobra.Command, args []string) error {
	svc := &DB2Service{store: store}
	return svc.dispatch
}

func (s *DB2Service) dispatch(cmd *cobra.Command, args []string) error {
	use := cmd.Name()
	switch {
	case containsWord(cmd.CommandPath(), "query schema") || containsWord(use, "schema"):
		return s.handleSchema(cmd, args)
	case containsWord(cmd.CommandPath(), "query rows") || containsWord(use, "rows"):
		return s.handleRows(cmd, args)
	case containsWord(cmd.CommandPath(), "query search") || containsWord(use, "search"):
		return s.handleSearch(cmd, args)
	case containsWord(cmd.CommandPath(), "query foreign-key") || containsWord(use, "foreign-key"):
		return s.handleForeignKey(cmd, args)
	case containsWord(cmd.CommandPath(), "query stream") || containsWord(use, "stream"):
		return s.handleStream(cmd, args)
	default:
		return s.handleList(cmd, args)
	}
}

func (s *DB2Service) handleSchema(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query schema", "missing_argument", "table name required"))
	}
	if db2StoreNotReady(s.store) {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query schema", "not_ready", warmupRequiredMessage))
	}
	if s.store != nil {
		fields, rowCount, err := s.store.Schema(args[0])
		if err != nil {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query schema", "query_error", err.Error()))
		}
		fieldMap := make(map[string]string, len(fields))
		for _, field := range fields {
			fieldType := field.Type
			if field.ArrayLen > 0 {
				fieldType = fmt.Sprintf("%s[%d]", fieldType, field.ArrayLen)
			}
			fieldMap[field.Name] = fieldType
		}
		return writeJSON(cmd.OutOrStdout(), NewSuccessResponse("query schema", map[string]interface{}{
			"table":    args[0],
			"rowCount": rowCount,
			"fields":   fieldMap,
		}))
	}
	return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query schema", "not_ready", warmupRequiredMessage))
}

func (s *DB2Service) handleRows(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query rows", "missing_argument", "table name required"))
	}

	idFlag, _ := cmd.Flags().GetString("id")
	idsFlag, _ := cmd.Flags().GetString("ids")
	limitFlag, _ := cmd.Flags().GetInt("limit")
	fieldsFlag, _ := cmd.Flags().GetString("fields")
	filterFlag, _ := cmd.Flags().GetString("filter")

	if db2StoreNotReady(s.store) {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query rows", "not_ready", warmupRequiredMessage))
	}
	if s.store != nil {
		ids, err := parseUint32CSV(idFlag, idsFlag)
		if err != nil {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query rows", "invalid_argument", err.Error()))
		}
		rows, err := s.store.Rows(args[0], ids, splitCSV(fieldsFlag), filterFlag, limitFlag)
		if err != nil {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query rows", "query_error", err.Error()))
		}
		return writeJSON(cmd.OutOrStdout(), NewSuccessResponse("query rows", map[string]interface{}{
			"table": args[0],
			"mode":  "rows",
			"count": len(rows),
			"rows":  rows,
		}))
	}
	return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query rows", "not_ready", warmupRequiredMessage))
}

func (s *DB2Service) handleSearch(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query search", "missing_argument", "table name required"))
	}

	fieldFlag, _ := cmd.Flags().GetString("field")
	queryFlag, _ := cmd.Flags().GetString("query")
	limitFlag, _ := cmd.Flags().GetInt("limit")

	if db2StoreNotReady(s.store) {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query search", "not_ready", warmupRequiredMessage))
	}
	if s.store != nil {
		rows, err := s.store.Search(args[0], fieldFlag, queryFlag, limitFlag)
		if err != nil {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query search", "query_error", err.Error()))
		}
		return writeJSON(cmd.OutOrStdout(), NewSuccessResponse("query search", map[string]interface{}{
			"table": args[0],
			"mode":  "search",
			"count": len(rows),
			"rows":  rows,
		}))
	}
	return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query search", "not_ready", warmupRequiredMessage))
}

func (s *DB2Service) handleForeignKey(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query foreign-key", "missing_argument", "table name required"))
	}

	fieldFlag, _ := cmd.Flags().GetString("field")
	valueFlag, _ := cmd.Flags().GetUint32("value")

	if db2StoreNotReady(s.store) {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query foreign-key", "not_ready", warmupRequiredMessage))
	}
	if s.store != nil {
		rows, err := s.store.ForeignKey(args[0], fieldFlag, valueFlag, 0)
		if err != nil {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query foreign-key", "query_error", err.Error()))
		}
		return writeJSON(cmd.OutOrStdout(), NewSuccessResponse("query foreign-key", map[string]interface{}{
			"table": args[0],
			"mode":  "foreign-key",
			"count": len(rows),
			"rows":  rows,
		}))
	}
	return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query foreign-key", "not_ready", warmupRequiredMessage))
}

func (s *DB2Service) handleStream(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query stream", "missing_argument", "table name required"))
	}

	limitFlag, _ := cmd.Flags().GetInt("limit")
	fieldsFlag, _ := cmd.Flags().GetString("fields")
	filterFlag, _ := cmd.Flags().GetString("filter")
	formatFlag, _ := cmd.Flags().GetString("format")

	if db2StoreNotReady(s.store) {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query stream", "not_ready", warmupRequiredMessage))
	}
	if s.store != nil {
		rows, err := s.store.Stream(args[0], splitCSV(fieldsFlag), filterFlag, limitFlag)
		if err != nil {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query stream", "query_error", err.Error()))
		}
		if formatFlag == "json" {
			return writeJSON(cmd.OutOrStdout(), NewSuccessResponse("query stream", map[string]interface{}{
				"table": args[0],
				"mode":  "stream",
				"count": len(rows),
				"rows":  rows,
			}))
		}
		if formatFlag != "" && formatFlag != "jsonl" {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query stream", "invalid_argument", "--format must be jsonl or json"))
		}
		for _, row := range rows {
			if err := writeJSONLine(cmd.OutOrStdout(), NewSuccessResponse("query stream row", map[string]interface{}{
				"table": args[0],
				"mode":  "stream",
				"row":   row,
			})); err != nil {
				return err
			}
		}
		return nil
	}
	return writeJSON(cmd.OutOrStdout(), NewErrorResponse("query stream", "not_ready", warmupRequiredMessage))
}

func writeJSONLine(w interface{ Write([]byte) (int, error) }, resp Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

type readyDB2Store interface {
	Ready() bool
}

func db2StoreNotReady(store appruntime.DB2Store) bool {
	if store == nil {
		return true
	}
	if ready, ok := store.(readyDB2Store); ok {
		return !ready.Ready()
	}
	return false
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseUint32CSV(values ...string) ([]uint32, error) {
	var out []uint32
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			n, err := strconv.ParseUint(part, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid uint32 ID %q", part)
			}
			out = append(out, uint32(n))
		}
	}
	return out, nil
}

func (s *DB2Service) handleList(cmd *cobra.Command, args []string) error {
	resp := NewSuccessResponse("query", map[string]interface{}{
		"note": "use query subcommands: schema, rows, search, foreign-key, stream",
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
