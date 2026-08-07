package app

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"wowdata/internal/sqlquery"

	"github.com/spf13/cobra"
)

const SQLPreparedQueryAnnotation = "wowdata.sql.prepared-query"

type SQLService struct{ engine *sqlquery.Engine }

func NewSQLHandler(source sqlquery.Source) func(cmd *cobra.Command, args []string) error {
	return (&SQLService{engine: &sqlquery.Engine{Source: source}}).handle
}

func (s *SQLService) handle(cmd *cobra.Command, args []string) error {
	query, err := sqlText(cmd, args)
	if err != nil {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("sql", "sql_invalid_input", err.Error()))
	}
	st, err := sqlquery.Parse(query)
	if err != nil {
		return writeSQLError(cmd, err)
	}
	params, err := sqlParams(cmd)
	if err != nil {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("sql", "sql_invalid_parameter", err.Error()))
	}
	format, _ := cmd.Flags().GetString("format")
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "json":
		res, err := s.engine.Execute(cmd.Context(), st, params)
		if err != nil {
			return writeSQLError(cmd, err)
		}
		return writeJSON(cmd.OutOrStdout(), NewSuccessResponse("sql", map[string]interface{}{
			"dialect": res.Dialect, "catalog": res.Catalog, "query": query, "columns": res.Columns, "rows": res.Rows, "rowCount": res.Count, "plan": res.Plan, "metrics": res.Metrics,
		}))
	case "jsonl":
		_, err := s.engine.ExecuteStream(cmd.Context(), st, params, func(_ []string, row map[string]any) error {
			if err := writeJSONLine(cmd.OutOrStdout(), NewSuccessResponse("sql row", map[string]interface{}{"dialect": sqlquery.Dialect, "catalog": "static", "row": row})); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return writeSQLError(cmd, err)
		}
		return nil
	case "csv":
		return writeSQLCSVStream(cmd.OutOrStdout(), s.engine, cmd, st, params)
	default:
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("sql", "sql_invalid_format", "--format must be json, jsonl, or csv"))
	}
}

func writeSQLCSVStream(w io.Writer, engine *sqlquery.Engine, cmd *cobra.Command, st *sqlquery.Statement, params sqlquery.Parameters) error {
	cw := csv.NewWriter(w)
	wroteHeader := false
	var columns []string
	res, err := engine.ExecuteStream(cmd.Context(), st, params, func(cols []string, row map[string]any) error {
		if !wroteHeader {
			columns = append([]string(nil), cols...)
			if len(columns) == 0 {
				for k := range row {
					columns = append(columns, k)
				}
				sort.Strings(columns)
			}
			if err := cw.Write(columns); err != nil {
				return err
			}
			wroteHeader = true
		}
		record := make([]string, len(columns))
		for i, c := range columns {
			if row[c] != nil {
				record[i] = fmt.Sprint(row[c])
			}
		}
		return cw.Write(record)
	})
	if err != nil {
		return err
	}
	if !wroteHeader && len(res.Columns) > 0 {
		if err := cw.Write(res.Columns); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func sqlText(cmd *cobra.Command, args []string) (string, error) {
	if cmd.Annotations != nil && cmd.Annotations[SQLPreparedQueryAnnotation] != "" {
		return cmd.Annotations[SQLPreparedQueryAnnotation], nil
	}
	file, _ := cmd.Flags().GetString("file")
	stdin, _ := cmd.Flags().GetBool("stdin")
	sources := 0
	if len(args) > 0 {
		sources++
	}
	if strings.TrimSpace(file) != "" {
		sources++
	}
	if stdin {
		sources++
	}
	if sources != 1 {
		return "", fmt.Errorf("provide exactly one query source: argument, --file, or --stdin")
	}
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	if stdin {
		b, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return strings.Join(args, " "), nil
}

func sqlParams(cmd *cobra.Command) (sqlquery.Parameters, error) {
	values, _ := cmd.Flags().GetStringArray("param")
	out := sqlquery.Parameters{}
	for _, item := range values {
		parts := strings.SplitN(item, "=", 2)
		name := strings.TrimSpace(parts[0])
		if len(parts) != 2 || name == "" {
			return nil, fmt.Errorf("invalid --param %q, expected name=value", item)
		}
		if _, ok := out[name]; ok {
			return nil, fmt.Errorf("duplicate parameter %q", name)
		}
		out[name] = inferSQLValue(strings.TrimSpace(parts[1]))
	}
	return out, nil
}

func inferSQLValue(v string) any {
	if strings.EqualFold(v, "null") {
		return nil
	}
	if b, e := strconv.ParseBool(v); e == nil {
		return b
	}
	if i, e := strconv.ParseInt(v, 10, 64); e == nil {
		return i
	}
	if f, e := strconv.ParseFloat(v, 64); e == nil {
		return f
	}
	if len(v) >= 2 && ((v[0] == '\'' && v[len(v)-1] == '\'') || (v[0] == '"' && v[len(v)-1] == '"')) {
		return v[1 : len(v)-1]
	}
	return v
}

func writeSQLError(cmd *cobra.Command, err error) error {
	if e, ok := err.(*sqlquery.Error); ok {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("sql", e.Code, e.Error()))
	}
	return writeJSON(cmd.OutOrStdout(), NewErrorResponse("sql", "sql_query_error", err.Error()))
}

func writeSQLCSV(w io.Writer, res sqlquery.Result) error {
	cols := append([]string(nil), res.Columns...)
	if len(cols) == 0 && len(res.Rows) > 0 {
		for k := range res.Rows[0] {
			cols = append(cols, k)
		}
		sort.Strings(cols)
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(cols); err != nil {
		return err
	}
	for _, row := range res.Rows {
		record := make([]string, len(cols))
		for i, c := range cols {
			if row[c] != nil {
				record[i] = fmt.Sprint(row[c])
			}
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
