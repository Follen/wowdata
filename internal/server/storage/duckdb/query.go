package duckdb

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var safeIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type QueryBuilder struct {
	trustedRoot string
}

type TableRef struct {
	TableName   string
	ParquetPath string
}

type RowsQuery struct {
	Columns []string
	Where   map[string]interface{}
	Limit   int
	Offset  int
}

type SearchQuery struct {
	Columns      []string
	SearchColumn string
	Pattern      string
	Limit        int
}

type ForeignKeyQuery struct {
	Columns []string
	Field   string
	Value   interface{}
	Limit   int
}

type StreamQuery struct {
	Columns []string
	Limit   int
	Offset  int
}

func NewQueryBuilder(trustedRoot string) QueryBuilder {
	return QueryBuilder{trustedRoot: trustedRoot}
}

func (b QueryBuilder) BuildRowsSQL(table TableRef, query RowsQuery) (string, []interface{}, error) {
	from, args, err := b.selectPrefix(table, query.Columns)
	if err != nil {
		return "", nil, err
	}

	where, whereArgs, err := equalityWhere(query.Where)
	if err != nil {
		return "", nil, err
	}
	args = append(args, whereArgs...)
	sql := from + where
	sql, args = appendLimitOffset(sql, args, query.Limit, query.Offset)
	return sql, args, nil
}

func (b QueryBuilder) BuildSearchSQL(table TableRef, query SearchQuery) (string, []interface{}, error) {
	from, args, err := b.selectPrefix(table, query.Columns)
	if err != nil {
		return "", nil, err
	}
	column, err := quoteIdentifier(query.SearchColumn)
	if err != nil {
		return "", nil, err
	}
	args = append(args, query.Pattern)
	sql := fmt.Sprintf("%s WHERE %s ILIKE ?", from, column)
	sql, args = appendLimitOffset(sql, args, query.Limit, 0)
	return sql, args, nil
}

func (b QueryBuilder) BuildForeignKeySQL(table TableRef, query ForeignKeyQuery) (string, []interface{}, error) {
	from, args, err := b.selectPrefix(table, query.Columns)
	if err != nil {
		return "", nil, err
	}
	field, err := quoteIdentifier(query.Field)
	if err != nil {
		return "", nil, err
	}
	args = append(args, query.Value)
	sql := fmt.Sprintf("%s WHERE %s = ?", from, field)
	sql, args = appendLimitOffset(sql, args, query.Limit, 0)
	return sql, args, nil
}

func (b QueryBuilder) BuildStreamSQL(table TableRef, query StreamQuery) (string, []interface{}, error) {
	from, args, err := b.selectPrefix(table, query.Columns)
	if err != nil {
		return "", nil, err
	}
	sql, args := appendLimitOffset(from, args, query.Limit, query.Offset)
	return sql, args, nil
}

func (b QueryBuilder) BuildSchemaSQL(table TableRef) (string, []interface{}, error) {
	tableName, path, err := b.trustedTable(table)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("DESCRIBE SELECT * FROM read_parquet(?) AS %s", tableName), []interface{}{path}, nil
}

func (b QueryBuilder) selectPrefix(table TableRef, columns []string) (string, []interface{}, error) {
	tableName, path, err := b.trustedTable(table)
	if err != nil {
		return "", nil, err
	}
	selectList, err := selectColumns(columns)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("SELECT %s FROM read_parquet(?) AS %s", selectList, tableName), []interface{}{path}, nil
}

func (b QueryBuilder) trustedTable(table TableRef) (string, string, error) {
	tableName, err := quoteIdentifier(table.TableName)
	if err != nil {
		return "", "", err
	}
	path, err := b.trustedPath(table.ParquetPath)
	if err != nil {
		return "", "", err
	}
	return tableName, path, nil
}

func (b QueryBuilder) trustedPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("parquet path is required")
	}
	if containsTraversal(path) {
		return "", fmt.Errorf("parquet path contains traversal: %q", path)
	}
	root, err := filepath.Abs(filepath.Clean(b.trustedRoot))
	if err != nil {
		return "", err
	}
	cleanPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, cleanPath)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("parquet path %q is outside trusted root %q", path, b.trustedRoot)
	}
	return cleanPath, nil
}

func selectColumns(columns []string) (string, error) {
	if len(columns) == 0 {
		return "*", nil
	}
	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		value, err := quoteIdentifier(column)
		if err != nil {
			return "", err
		}
		quoted = append(quoted, value)
	}
	return strings.Join(quoted, ", "), nil
}

func equalityWhere(where map[string]interface{}) (string, []interface{}, error) {
	if len(where) == 0 {
		return "", nil, nil
	}
	fields := make([]string, 0, len(where))
	for field := range where {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	clauses := make([]string, 0, len(fields))
	args := make([]interface{}, 0, len(fields))
	for _, field := range fields {
		column, err := quoteIdentifier(field)
		if err != nil {
			return "", nil, err
		}
		clauses = append(clauses, column+" = ?")
		args = append(args, where[field])
	}
	return " WHERE " + strings.Join(clauses, " AND "), args, nil
}

func appendLimitOffset(sql string, args []interface{}, limit int, offset int) (string, []interface{}) {
	if limit > 0 {
		sql += " LIMIT ?"
		args = append(args, limit)
	}
	if offset > 0 {
		sql += " OFFSET ?"
		args = append(args, offset)
	}
	return sql, args
}

func quoteIdentifier(value string) (string, error) {
	if !safeIdentifierPattern.MatchString(value) {
		return "", fmt.Errorf("invalid identifier %q", value)
	}
	return `"` + value + `"`, nil
}

func containsTraversal(path string) bool {
	parts := strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	for _, part := range parts {
		if part == ".." {
			return true
		}
	}
	return false
}
