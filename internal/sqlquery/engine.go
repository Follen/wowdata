package sqlquery

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Row map[string]any

type Column struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	ID           bool   `json:"id,omitempty"`
	Relationship bool   `json:"relationship,omitempty"`
}

type TableInfo struct {
	Name     string   `json:"name"`
	Columns  []Column `json:"columns"`
	RowCount int      `json:"rowCount"`
}

type AccessStats struct {
	Physical      string `json:"physical"`
	InputRows     int    `json:"inputRows"`
	VisitedRows   int    `json:"visitedRows"`
	ScannedRows   int    `json:"scannedRows"`
	DecodedRows   int    `json:"decodedRows"`
	DecodedFields int    `json:"decodedFields"`
	OutputRows    int    `json:"outputRows"`
	BatchCount    int    `json:"batchCount"`
	ReadBytes     int64  `json:"readBytes,omitempty"`
	Fallback      string `json:"fallback,omitempty"`
}

type Source interface {
	Describe(ctx context.Context, table string) (TableInfo, error)
	Lookup(ctx context.Context, table string, ids []uint32, fields []string) ([]Row, AccessStats, error)
	Relationship(ctx context.Context, table, field string, values []uint32, fields []string) ([]Row, AccessStats, error)
	Scan(ctx context.Context, table string, fields []string, limit int) ([]Row, AccessStats, error)
}

// StreamSource is the optional bounded scan capability used by JSONL/CSV.
// Implementations must invoke yield as rows are decoded and stop immediately
// when it returns an error; they must not materialize the whole table first.
type StreamSource interface {
	ScanStream(ctx context.Context, table string, fields []string, limit int, yield func(Row) error) (AccessStats, error)
}

// PredicateScanSource is an optional physical equality/filter pushdown. The
// engine rechecks the predicate after retrieval, so a source may only use it
// to reduce decoding and stop once the requested number of matches is found.
type PredicateScanSource interface {
	ScanFilter(ctx context.Context, table string, fields []string, predicate func(Row) bool, limit int) ([]Row, AccessStats, error)
}

// TextSearchSource is an optional physical fast path for the common
// LOWER(column) LIKE '%literal%' predicate used by atomic search commands.
type TextSearchSource interface {
	TextSearch(ctx context.Context, table, field, query string, caseSensitive bool, fields []string, limit int) ([]Row, AccessStats, error)
}

type Parameters map[string]any

type OperatorStats struct {
	Name          string        `json:"name"`
	Physical      string        `json:"physical"`
	EstimatedRows int           `json:"estimatedRows,omitempty"`
	InputRows     int           `json:"inputRows,omitempty"`
	VisitedRows   int           `json:"visitedRows,omitempty"`
	ScannedRows   int           `json:"scannedRows,omitempty"`
	DecodedRows   int           `json:"decodedRows,omitempty"`
	DecodedFields int           `json:"decodedFields,omitempty"`
	OutputRows    int           `json:"outputRows,omitempty"`
	BatchCount    int           `json:"batchCount,omitempty"`
	ReadBytes     int64         `json:"readBytes,omitempty"`
	SpillBytes    int64         `json:"spillBytes,omitempty"`
	PeakBytes     int64         `json:"peakBytes,omitempty"`
	HashEntries   int           `json:"hashEntries,omitempty"`
	BuildSide     string        `json:"buildSide,omitempty"`
	ProbeSide     string        `json:"probeSide,omitempty"`
	AllocBytes    uint64        `json:"allocBytes,omitempty"`
	Duration      time.Duration `json:"duration,omitempty"`
	Fallback      string        `json:"fallback,omitempty"`
}

type Plan struct {
	Dialect   string          `json:"dialect"`
	Catalog   string          `json:"catalog"`
	Logical   []string        `json:"logical"`
	Physical  []string        `json:"physical"`
	Operators []OperatorStats `json:"operators,omitempty"`
}

type Metrics struct {
	Duration      time.Duration `json:"duration"`
	InputRows     int           `json:"inputRows"`
	VisitedRows   int           `json:"visitedRows"`
	ScannedRows   int           `json:"scannedRows"`
	DecodedRows   int           `json:"decodedRows"`
	DecodedFields int           `json:"decodedFields"`
	OutputRows    int           `json:"outputRows"`
	ReadBytes     int64         `json:"readBytes"`
	SpillBytes    int64         `json:"spillBytes"`
	PeakBytes     int64         `json:"peakBytes"`
	HotfixCalls   int           `json:"hotfixCalls"`
}

type Result struct {
	Dialect string           `json:"dialect"`
	Catalog string           `json:"catalog"`
	Columns []string         `json:"columns"`
	Rows    []map[string]any `json:"rows,omitempty"`
	Count   int              `json:"rowCount"`
	Plan    Plan             `json:"plan"`
	Metrics Metrics          `json:"metrics"`
}

type Engine struct {
	Source           Source
	MemoryLimitBytes int64
}

const defaultQueryMemoryLimit int64 = 768 << 20

func (e *Engine) Execute(ctx context.Context, st *Statement, params Parameters) (Result, error) {
	if e == nil || e.Source == nil {
		return Result{}, sqlErr("schema_not_available", Position{}, "static DB2 source is not available")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, sqlErr("sql_cancelled", Position{}, err.Error())
	}
	required := parameterNames(st.Query)
	for name := range required {
		if _, ok := params[name]; !ok {
			return Result{}, sqlErr("sql_missing_parameter", Position{}, "missing parameter :"+name)
		}
	}
	for name := range params {
		if !required[name] {
			return Result{}, sqlErr("sql_unknown_parameter", Position{}, "unknown parameter :"+name)
		}
	}
	start := time.Now()
	memoryLimit := e.MemoryLimitBytes
	if memoryLimit <= 0 {
		memoryLimit = defaultQueryMemoryLimit
	}
	env := &execEnv{ctx: ctx, source: e.Source, params: params, ctes: map[string]*Select{}, infos: map[string]TableInfo{}, planOnly: st.Explain && !st.Analyze, bindCTE: map[string]bool{}, cteRows: map[string][]map[string]any{}, recursiveCTE: map[string]bool{}, recursiveRunning: map[string]bool{}, memoryLimit: memoryLimit}
	for _, c := range st.Query.CTEs {
		name := strings.ToLower(c.Name)
		env.ctes[name] = c.Query
		if st.Query.Recursive && c.Query.UnionAll != nil {
			env.recursiveCTE[name] = true
		}
	}
	if err := env.bind(st.Query); err != nil {
		return Result{}, err
	}
	rows, err := env.runSelect(st.Query, nil)
	if err != nil {
		return Result{}, err
	}
	res := Result{Dialect: Dialect, Catalog: "static", Rows: rows, Count: len(rows), Plan: env.plan}
	res.Plan.Dialect = Dialect
	res.Plan.Catalog = "static"
	res.Metrics = env.metrics
	res.Metrics.Duration = time.Since(start)
	res.Metrics.OutputRows = len(rows)
	if len(rows) > 0 {
		for k := range rows[0] {
			res.Columns = append(res.Columns, k)
		}
		sort.Strings(res.Columns)
	} else {
		res.Columns = projectionNames(st.Query, env)
	}
	if st.Explain && !st.Analyze {
		res.Rows = nil
		res.Count = 0
		res.Metrics = Metrics{}
	}
	return res, nil
}

var errStreamLimit = errors.New("sql stream limit reached")

// ExecuteStream streams the common single-table scan/filter/project/limit
// shape. Queries requiring global state (sort, distinct, aggregate, join,
// CTE/union, or EXPLAIN) use Execute and then yield their bounded result.
func (e *Engine) ExecuteStream(ctx context.Context, st *Statement, params Parameters, yield func([]string, map[string]any) error) (Result, error) {
	if yield == nil {
		return Result{}, sqlErr("sql_invalid_input", Position{}, "stream yield callback is required")
	}
	q := st.Query
	stream, ok := e.Source.(StreamSource)
	if !ok || st.Explain || len(q.CTEs) > 0 || q.UnionAll != nil || len(q.Joins) > 0 || q.Distinct || hasAggregate(q) || len(q.GroupBy) > 0 || len(q.OrderBy) > 0 || q.From.Subquery != nil {
		res, err := e.Execute(ctx, st, params)
		if err != nil {
			return Result{}, err
		}
		for _, row := range res.Rows {
			if err := yield(res.Columns, row); err != nil {
				return Result{}, err
			}
		}
		return res, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	required := parameterNames(q)
	for name := range required {
		if _, found := params[name]; !found {
			return Result{}, sqlErr("sql_missing_parameter", Position{}, "missing parameter :"+name)
		}
	}
	for name := range params {
		if !required[name] {
			return Result{}, sqlErr("sql_unknown_parameter", Position{}, "unknown parameter :"+name)
		}
	}
	started := time.Now()
	env := &execEnv{ctx: ctx, source: e.Source, params: params, ctes: map[string]*Select{}, infos: map[string]TableInfo{}, bindCTE: map[string]bool{}, cteRows: map[string][]map[string]any{}, recursiveCTE: map[string]bool{}, recursiveRunning: map[string]bool{}, memoryLimit: e.MemoryLimitBytes}
	if env.memoryLimit <= 0 {
		env.memoryLimit = defaultQueryMemoryLimit
	}
	if err := env.bind(q); err != nil {
		return Result{}, err
	}
	info, err := e.Source.Describe(ctx, q.From.Name)
	if err != nil {
		return Result{}, err
	}
	alias := q.From.EffectiveAlias()
	fields := requiredFields(q, alias, info)
	columns := projectionNames(q, env)
	target := 0
	if q.Limit != nil {
		target = q.Offset + *q.Limit
	}
	seen, emitted := 0, 0
	stats, err := stream.ScanStream(ctx, q.From.Name, fields, 0, func(raw Row) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		row := qualify(raw, alias)
		if q.Where != nil {
			value := env.eval(q.Where, row, nil, nil)
			if env.evalErr != nil {
				err := env.evalErr
				env.evalErr = nil
				return err
			}
			ok, err := truth(value)
			if err != nil || ok == nil || !*ok {
				return err
			}
		}
		seen++
		if seen <= q.Offset {
			return nil
		}
		projected, err := env.projectOne(q, row, nil)
		if err != nil {
			return err
		}
		if err := yield(columns, projected); err != nil {
			return err
		}
		emitted++
		if target > 0 && seen >= target {
			return errStreamLimit
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStreamLimit) {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Result{}, sqlErr("sql_cancelled", q.Pos, err.Error())
		}
		return Result{}, err
	}
	env.addOp(OperatorStats{Name: q.From.Name, Physical: "WDCStreamingColumnBatchScan", EstimatedRows: info.RowCount, InputRows: stats.InputRows, VisitedRows: stats.VisitedRows, ScannedRows: stats.ScannedRows, DecodedRows: stats.DecodedRows, DecodedFields: stats.DecodedFields, OutputRows: emitted, BatchCount: stats.BatchCount, ReadBytes: stats.ReadBytes, Fallback: stats.Fallback})
	env.addOp(OperatorStats{Name: "filter_project_limit", Physical: "StreamingFilterProjectLimitFusion", InputRows: seen, OutputRows: emitted, BatchCount: maxInt(1, (seen+255)/256), PeakBytes: 256 * 96})
	res := Result{Dialect: Dialect, Catalog: "static", Columns: columns, Count: emitted, Plan: env.plan, Metrics: env.metrics}
	res.Plan.Dialect, res.Plan.Catalog = Dialect, "static"
	res.Metrics.Duration = time.Since(started)
	res.Metrics.OutputRows = emitted
	return res, nil
}

func parameterNames(q *Select) map[string]bool {
	out := map[string]bool{}
	var walkSelect func(*Select)
	var walk func(Expr)
	walk = func(x Expr) {
		switch v := x.(type) {
		case *Parameter:
			out[v.Name] = true
		case *UnaryExpr:
			walk(v.X)
		case *BinaryExpr:
			walk(v.Left)
			walk(v.Right)
		case *InExpr:
			walk(v.X)
			for _, a := range v.List {
				walk(a)
			}
			if v.Query != nil {
				walkSelect(v.Query)
			}
		case *BetweenExpr:
			walk(v.X)
			walk(v.Low)
			walk(v.High)
		case *IsNullExpr:
			walk(v.X)
		case *CallExpr:
			for _, a := range v.Args {
				walk(a)
			}
		case *CaseExpr:
			walk(v.Base)
			for _, w := range v.Whens {
				walk(w.When)
				walk(w.Then)
			}
			walk(v.Else)
		case *CastExpr:
			walk(v.X)
		case *ExistsExpr:
			walkSelect(v.Query)
		}
	}
	walkSelect = func(s *Select) {
		if s == nil {
			return
		}
		for _, c := range s.CTEs {
			walkSelect(c.Query)
		}
		for _, it := range s.Items {
			walk(it.Expr)
		}
		walk(s.Where)
		walk(s.Having)
		for _, x := range s.GroupBy {
			walk(x)
		}
		for _, x := range s.OrderBy {
			walk(x.Expr)
		}
		for _, j := range s.Joins {
			walk(j.On)
			if j.Table.Subquery != nil {
				walkSelect(j.Table.Subquery)
			}
		}
		if s.From.Subquery != nil {
			walkSelect(s.From.Subquery)
		}
		if s.UnionAll != nil {
			walkSelect(s.UnionAll)
		}
	}
	walkSelect(q)
	return out
}

type execEnv struct {
	ctx              context.Context
	source           Source
	params           Parameters
	ctes             map[string]*Select
	infos            map[string]TableInfo
	plan             Plan
	metrics          Metrics
	planOnly         bool
	bindCTE          map[string]bool
	cteRows          map[string][]map[string]any
	recursiveCTE     map[string]bool
	recursiveRunning map[string]bool
	evalErr          error
	memoryLimit      int64
	peakBytes        int64
}

type tableBinding struct {
	alias, name string
	info        TableInfo
}

func (e *execEnv) bind(q *Select) error {
	bindings := map[string]tableBinding{}
	add := func(t TableRef) error {
		if t.Catalog != "" && !strings.EqualFold(t.Catalog, "static") {
			return sqlErr("sql_unknown_catalog", t.Pos, "only static catalog is available")
		}
		alias := strings.ToLower(t.EffectiveAlias())
		if _, ok := bindings[alias]; ok {
			return sqlErr("sql_invalid_join", t.Pos, "duplicate table alias "+alias)
		}
		if t.Subquery != nil {
			if err := e.bind(t.Subquery); err != nil {
				return err
			}
			info := projectionInfo(t.Subquery)
			bindings[alias] = tableBinding{alias: alias, name: alias, info: info}
			e.infos[alias] = info
			return nil
		}
		name := strings.ToLower(t.Name)
		if cq := e.ctes[name]; cq != nil {
			info := projectionInfo(cq)
			bindings[alias] = tableBinding{alias: alias, name: t.Name, info: info}
			e.infos[alias] = info
			if e.bindCTE[name] {
				return nil
			}
			e.bindCTE[name] = true
			err := e.bind(cq)
			delete(e.bindCTE, name)
			return err
		}
		info, err := e.source.Describe(e.ctx, t.Name)
		if err != nil {
			return sqlErr("sql_unknown_table", t.Pos, err.Error())
		}
		bindings[alias] = tableBinding{alias: alias, name: t.Name, info: info}
		e.infos[alias] = info
		return nil
	}
	if err := add(q.From); err != nil {
		return err
	}
	for _, j := range q.Joins {
		if err := add(j.Table); err != nil {
			return err
		}
	}
	var check func(Expr) error
	check = func(x Expr) error {
		switch v := x.(type) {
		case nil, *Literal, *Parameter, *Star:
			return nil
		case *Identifier:
			matches := 0
			for alias, b := range bindings {
				if v.Qualifier != "" && !strings.EqualFold(v.Qualifier, alias) {
					continue
				}
				for _, c := range b.info.Columns {
					if strings.EqualFold(c.Name, v.Name) {
						matches++
						break
					}
				}
			}
			if matches == 0 {
				return sqlErr("sql_unknown_column", v.Pos, "unknown column "+v.Name)
			}
			if matches > 1 && v.Qualifier == "" {
				return sqlErr("sql_ambiguous_column", v.Pos, "ambiguous column "+v.Name)
			}
		case *UnaryExpr:
			return check(v.X)
		case *BinaryExpr:
			if err := check(v.Left); err != nil {
				return err
			}
			return check(v.Right)
		case *InExpr:
			if err := check(v.X); err != nil {
				return err
			}
			for _, a := range v.List {
				if err := check(a); err != nil {
					return err
				}
			}
			if v.Query != nil {
				return e.bind(v.Query)
			}
		case *BetweenExpr:
			for _, a := range []Expr{v.X, v.Low, v.High} {
				if err := check(a); err != nil {
					return err
				}
			}
		case *IsNullExpr:
			return check(v.X)
		case *CallExpr:
			for _, a := range v.Args {
				if _, ok := a.(*Star); ok {
					continue
				}
				if err := check(a); err != nil {
					return err
				}
			}
		case *CaseExpr:
			if v.Base != nil {
				if err := check(v.Base); err != nil {
					return err
				}
			}
			for _, w := range v.Whens {
				if err := check(w.When); err != nil {
					return err
				}
				if err := check(w.Then); err != nil {
					return err
				}
			}
			return check(v.Else)
		case *CastExpr:
			return check(v.X)
		case *ExistsExpr:
			return e.bind(v.Query)
		}
		return nil
	}
	for _, it := range q.Items {
		if err := check(it.Expr); err != nil {
			return err
		}
	}
	for _, j := range q.Joins {
		if err := check(j.On); err != nil {
			return err
		}
	}
	for _, x := range append(append([]Expr{q.Where, q.Having}, q.GroupBy...), orderExprs(q.OrderBy)...) {
		if err := check(x); err != nil {
			return err
		}
	}
	if q.UnionAll != nil {
		return e.bind(q.UnionAll)
	}
	return nil
}

func projectionInfo(q *Select) TableInfo {
	info := TableInfo{Name: "derived"}
	for i, it := range q.Items {
		name := it.Alias
		if name == "" {
			switch v := it.Expr.(type) {
			case *Identifier:
				name = v.Name
			case *Star:
				name = "*"
			default:
				name = fmt.Sprintf("expr_%d", i+1)
			}
		}
		info.Columns = append(info.Columns, Column{Name: name, Type: "any"})
	}
	return info
}

func orderExprs(in []OrderTerm) []Expr {
	out := make([]Expr, 0, len(in))
	for _, x := range in {
		out = append(out, x.Expr)
	}
	return out
}

func (e *execEnv) runSelect(q *Select, outer Row) ([]map[string]any, error) {
	base := *q
	base.UnionAll = nil
	rows, err := e.runSelectOne(&base, outer)
	if err != nil {
		return nil, err
	}
	if q.UnionAll != nil {
		right, err := e.runSelect(q.UnionAll, outer)
		if err != nil {
			return nil, err
		}
		rows = append(rows, right...)
		e.plan.Logical = append(e.plan.Logical, "LogicalUnionAll")
		e.addOp(OperatorStats{Name: "union_all", Physical: "AppendUnionAll", InputRows: len(rows), OutputRows: len(rows)})
	}
	return rows, nil
}

func (e *execEnv) runSelectOne(q *Select, outer Row) ([]map[string]any, error) {
	rows, err := e.readTable(q.From, q, outer)
	if err != nil {
		return nil, err
	}
	e.plan.Logical = append(e.plan.Logical, "LogicalScan")
	for _, j := range q.Joins {
		right, er := e.readTable(j.Table, q, outer)
		if er != nil {
			return nil, er
		}
		started := time.Now()
		before := len(rows) + len(right)
		var js joinStats
		joinLimit := 0
		if len(q.Joins) == 1 && q.Where == nil && !hasAggregate(q) && len(q.OrderBy) == 0 && !q.Distinct && q.Limit != nil {
			joinLimit = q.Offset + *q.Limit
		}
		rows, js, er = e.joinRows(rows, right, j, joinLimit)
		if er != nil {
			return nil, er
		}
		e.addOp(OperatorStats{Name: "join", Physical: js.physical, InputRows: before, OutputRows: len(rows), Duration: time.Since(started), PeakBytes: js.peakBytes, HashEntries: js.hashEntries, BuildSide: js.buildSide, ProbeSide: js.probeSide, Fallback: js.fallback})
		e.plan.Logical = append(e.plan.Logical, "LogicalJoin")
	}
	if q.Where != nil {
		started := time.Now()
		inputRows := len(rows)
		selection := make([]int, 0, minInt(len(rows), 4096))
		out := rows[:0]
		batches := 0
		for base := 0; base < len(rows); base += 256 {
			end := minInt(base+256, len(rows))
			selection = selection[:0]
			batches++
			for i := base; i < end; i++ {
				r := rows[i]
				if i&255 == 0 {
					if err := e.ctx.Err(); err != nil {
						return nil, sqlErr("sql_cancelled", q.Pos, err.Error())
					}
				}
				value := e.eval(q.Where, r, outer, nil)
				if e.evalErr != nil {
					err := e.evalErr
					e.evalErr = nil
					return nil, err
				}
				ok, er := truth(value)
				if er != nil {
					return nil, er
				}
				if ok != nil && *ok {
					selection = append(selection, i)
				}
			}
			for _, i := range selection {
				out = append(out, rows[i])
			}
		}
		rows = out
		e.addOp(OperatorStats{Name: "filter", Physical: "PredicateSelectionVector", InputRows: inputRows, OutputRows: len(rows), BatchCount: batches, PeakBytes: int64(cap(selection)) * 8, Duration: time.Since(started)})
		e.plan.Logical = append(e.plan.Logical, "LogicalFilter")
	}
	agg := hasAggregate(q) || len(q.GroupBy) > 0
	var projected []map[string]any
	if agg {
		projected, err = e.aggregate(q, rows, outer)
		e.plan.Logical = append(e.plan.Logical, "LogicalAggregate")
	} else {
		projected, err = e.project(q, rows, outer)
	}
	if err != nil {
		return nil, err
	}
	e.plan.Logical = append(e.plan.Logical, "LogicalProject")
	if q.Distinct {
		var peak int64
		projected, peak, err = e.distinctRows(projected, q.Pos)
		if err != nil {
			return nil, err
		}
		e.addOp(OperatorStats{Name: "distinct", Physical: "TypedHashDistinct", InputRows: len(rows), OutputRows: len(projected), PeakBytes: peak})
	}
	if q.Having != nil && agg { /* aggregate already evaluates HAVING */
	}
	if len(q.OrderBy) > 0 {
		started := time.Now()
		phys := "BoundedInMemorySort"
		if q.Limit != nil {
			phys = "TopKHeap"
			projected, err = e.topK(projected, q.OrderBy, q.Offset+*q.Limit, q.Pos)
		} else {
			if err = e.requireMemory(int64(len(projected))*96, q.Pos, "sort"); err == nil {
				sort.SliceStable(projected, func(i, j int) bool { return e.lessOrder(q.OrderBy, projected[i], projected[j]) })
			}
		}
		if err != nil {
			return nil, err
		}
		e.addOp(OperatorStats{Name: "sort", Physical: phys, InputRows: len(projected), OutputRows: len(projected), PeakBytes: int64(len(projected)) * 96, Duration: time.Since(started)})
		e.plan.Logical = append(e.plan.Logical, "LogicalSort")
	}
	start := q.Offset
	if start > len(projected) {
		start = len(projected)
	}
	end := len(projected)
	if q.Limit != nil && start+*q.Limit < end {
		end = start + *q.Limit
	}
	projected = projected[start:end]
	if q.Limit != nil || q.Offset > 0 {
		e.addOp(OperatorStats{Name: "limit", Physical: "LimitOperator", InputRows: len(rows), OutputRows: len(projected)})
		e.plan.Logical = append(e.plan.Logical, "LogicalLimit")
	}
	return projected, nil
}

func (e *execEnv) readTable(t TableRef, q *Select, outer Row) ([]Row, error) {
	alias := t.EffectiveAlias()
	var err error
	if t.Subquery != nil {
		rr, err := e.runSelect(t.Subquery, outer)
		if err != nil {
			return nil, err
		}
		return qualifyRows(rr, alias), nil
	}
	name := strings.ToLower(t.Name)
	if materialized, ok := e.cteRows[name]; ok {
		return qualifyRows(materialized, alias), nil
	}
	if cq := e.ctes[name]; cq != nil {
		if e.recursiveCTE[name] {
			rr, err := e.runRecursiveCTE(name, cq, outer)
			if err != nil {
				return nil, err
			}
			return qualifyRows(rr, alias), nil
		}
		rr, err := e.runSelect(cq, outer)
		if err != nil {
			return nil, err
		}
		return qualifyRows(rr, alias), nil
	}
	info, ok := e.infos[strings.ToLower(alias)]
	if !ok {
		info, err = e.source.Describe(e.ctx, t.Name)
		if err != nil {
			return nil, err
		}
	}
	fields := requiredFields(q, alias, info)
	access := detectAccess(q.Where, alias, info, e.params)
	if e.planOnly {
		physical := "WDCColumnBatchScan"
		if access.kind == "id" {
			physical = "WDCRecordIDLookup"
		}
		if access.kind == "relationship" {
			physical = "WDCRelationshipLookup"
		}
		e.addOp(OperatorStats{Name: t.Name, Physical: physical, EstimatedRows: info.RowCount})
		return nil, nil
	}
	var rows []Row
	var st AccessStats
	switch access.kind {
	case "id":
		rows, st, err = e.source.Lookup(e.ctx, t.Name, access.values, fields)
	case "relationship":
		rows, st, err = e.source.Relationship(e.ctx, t.Name, access.field, access.values, fields)
	case "search":
		search, supported := e.source.(TextSearchSource)
		if !supported {
			rows, st, err = e.source.Scan(e.ctx, t.Name, fields, 0)
			break
		}
		limit := 0
		if access.complete && len(q.Joins) == 0 && !hasAggregate(q) && len(q.OrderBy) == 0 && !q.Distinct && q.Limit != nil {
			limit = q.Offset + *q.Limit
		}
		rows, st, err = search.TextSearch(e.ctx, t.Name, access.field, access.text, access.caseSensitive, fields, limit)
	case "predicate":
		filter, supported := e.source.(PredicateScanSource)
		if !supported {
			rows, st, err = e.source.Scan(e.ctx, t.Name, fields, 0)
			break
		}
		limit := 0
		if access.complete && len(q.Joins) == 0 && !hasAggregate(q) && len(q.OrderBy) == 0 && !q.Distinct && q.Limit != nil {
			limit = q.Offset + *q.Limit
		}
		literal := access.literal
		rows, st, err = filter.ScanFilter(e.ctx, t.Name, fields, func(row Row) bool {
			return literal != nil && row[access.field] != nil && compare(row[access.field], literal) == 0
		}, limit)
	default:
		limit := 0
		if q.Where == nil && len(q.Joins) == 0 && !hasAggregate(q) && len(q.OrderBy) == 0 && !q.Distinct && q.Limit != nil {
			limit = q.Offset + *q.Limit
		}
		rows, st, err = e.source.Scan(e.ctx, t.Name, fields, limit)
	}
	if err != nil {
		return nil, err
	}
	if queryNeedsQualification(q, alias) {
		for i := range rows {
			rows[i] = qualify(rows[i], alias)
		}
	}
	e.addOp(OperatorStats{Name: t.Name, Physical: st.Physical, EstimatedRows: info.RowCount, InputRows: st.InputRows, VisitedRows: st.VisitedRows, ScannedRows: st.ScannedRows, DecodedRows: st.DecodedRows, DecodedFields: st.DecodedFields, OutputRows: len(rows), BatchCount: st.BatchCount, ReadBytes: st.ReadBytes, Fallback: st.Fallback})
	return rows, nil
}

func (e *execEnv) runRecursiveCTE(name string, q *Select, outer Row) ([]map[string]any, error) {
	if e.recursiveRunning[name] {
		return nil, sqlErr("sql_recursive_limit", q.Pos, "recursive CTE re-entered without frontier")
	}
	e.recursiveRunning[name] = true
	defer func() { delete(e.recursiveRunning, name); delete(e.cteRows, name) }()
	anchor := *q
	anchor.UnionAll = nil
	all, err := e.runSelect(&anchor, outer)
	if err != nil {
		return nil, err
	}
	frontier := append([]map[string]any(nil), all...)
	for iteration := 0; iteration < 1000 && len(frontier) > 0; iteration++ {
		e.cteRows[name] = frontier
		next, err := e.runSelect(q.UnionAll, outer)
		if err != nil {
			return nil, err
		}
		if len(next) == 0 {
			return all, nil
		}
		all = append(all, next...)
		frontier = next
	}
	if len(frontier) > 0 {
		return nil, sqlErr("sql_recursive_limit", q.Pos, "recursive CTE exceeded 1000 iterations")
	}
	return all, nil
}

type accessPlan struct {
	kind, field   string
	values        []uint32
	text          string
	literal       any
	caseSensitive bool
	complete      bool
}

func detectAccess(x Expr, alias string, info TableInfo, params Parameters) accessPlan {
	var id string
	rels := map[string]bool{}
	for _, c := range info.Columns {
		if c.ID || strings.EqualFold(c.Name, "ID") {
			id = c.Name
		}
		if c.Relationship {
			rels[strings.ToLower(c.Name)] = true
		}
	}
	var find func(Expr) accessPlan
	find = func(e Expr) accessPlan {
		switch v := e.(type) {
		case *BinaryExpr:
			if strings.EqualFold(v.Op, "AND") {
				a := find(v.Left)
				if a.kind != "" {
					return a
				}
				return find(v.Right)
			}
			if v.Op == "=" {
				if col, ok := v.Left.(*Identifier); ok && (col.Qualifier == "" || strings.EqualFold(col.Qualifier, alias)) {
					literal := literalValue(v.Right, params)
					if n, yes := uintValue(literal); yes {
						if strings.EqualFold(col.Name, id) {
							return accessPlan{kind: "id", field: col.Name, values: []uint32{n}}
						}
						if rels[strings.ToLower(col.Name)] {
							return accessPlan{kind: "relationship", field: col.Name, values: []uint32{n}}
						}
					}
					if literal != nil {
						return accessPlan{kind: "predicate", field: col.Name, literal: literal}
					}
				}
				if col, ok := v.Right.(*Identifier); ok && (col.Qualifier == "" || strings.EqualFold(col.Qualifier, alias)) {
					if literal := literalValue(v.Left, params); literal != nil {
						return accessPlan{kind: "predicate", field: col.Name, literal: literal}
					}
				}
			}
			if strings.EqualFold(v.Op, "LIKE") {
				if plan, ok := detectTextSearch(v, alias, params); ok {
					return plan
				}
			}
		case *InExpr:
			if v.Query != nil || len(v.List) == 0 {
				return accessPlan{}
			}
			if col, ok := v.X.(*Identifier); ok && !v.Not && (col.Qualifier == "" || strings.EqualFold(col.Qualifier, alias)) {
				var vals []uint32
				for _, a := range v.List {
					n, yes := uintValue(literalValue(a, params))
					if !yes {
						return accessPlan{}
					}
					vals = append(vals, n)
				}
				if strings.EqualFold(col.Name, id) {
					return accessPlan{kind: "id", field: col.Name, values: vals}
				}
				if rels[strings.ToLower(col.Name)] {
					return accessPlan{kind: "relationship", field: col.Name, values: vals}
				}
			}
		}
		return accessPlan{}
	}
	plan := find(x)
	plan.complete = plan.kind != "" && x != nil && isSameAccessPredicate(x, plan, alias, params)
	return plan
}

func detectTextSearch(v *BinaryExpr, alias string, params Parameters) (accessPlan, bool) {
	pattern, ok := literalValue(v.Right, params).(string)
	if !ok || len(pattern) < 2 || pattern[0] != '%' || pattern[len(pattern)-1] != '%' {
		return accessPlan{}, false
	}
	needle := pattern[1 : len(pattern)-1]
	if strings.ContainsAny(needle, "%_") {
		return accessPlan{}, false
	}
	call, ok := v.Left.(*CallExpr)
	if !ok || !strings.EqualFold(call.Name, "LOWER") || len(call.Args) != 1 {
		return accessPlan{}, false
	}
	col, ok := call.Args[0].(*Identifier)
	if !ok || (col.Qualifier != "" && !strings.EqualFold(col.Qualifier, alias)) {
		return accessPlan{}, false
	}
	return accessPlan{kind: "search", field: col.Name, text: strings.ToLower(needle)}, true
}

func isSameAccessPredicate(x Expr, plan accessPlan, alias string, params Parameters) bool {
	switch plan.kind {
	case "search":
		v, ok := x.(*BinaryExpr)
		if !ok {
			return false
		}
		_, ok = detectTextSearch(v, alias, params)
		return ok
	case "id", "relationship":
		switch x.(type) {
		case *BinaryExpr, *InExpr:
			return true
		}
	case "predicate":
		v, ok := x.(*BinaryExpr)
		return ok && v.Op == "="
	}
	return false
}

func queryNeedsQualification(q *Select, alias string) bool {
	if len(q.Joins) > 0 {
		return true
	}
	needed := false
	var walk func(Expr)
	walk = func(x Expr) {
		if needed || x == nil {
			return
		}
		switch v := x.(type) {
		case *Identifier:
			needed = v.Qualifier != "" && strings.EqualFold(v.Qualifier, alias)
		case *Star:
			needed = v.Qualifier != "" && strings.EqualFold(v.Qualifier, alias)
		case *UnaryExpr:
			walk(v.X)
		case *BinaryExpr:
			walk(v.Left)
			walk(v.Right)
		case *InExpr:
			walk(v.X)
			for _, a := range v.List {
				walk(a)
			}
		case *BetweenExpr:
			walk(v.X)
			walk(v.Low)
			walk(v.High)
		case *IsNullExpr:
			walk(v.X)
		case *CallExpr:
			for _, a := range v.Args {
				walk(a)
			}
		case *CastExpr:
			walk(v.X)
		}
	}
	for _, item := range q.Items {
		walk(item.Expr)
	}
	walk(q.Where)
	walk(q.Having)
	for _, term := range q.OrderBy {
		walk(term.Expr)
	}
	for _, expr := range q.GroupBy {
		walk(expr)
	}
	return needed
}

func literalValue(x Expr, p Parameters) any {
	switch v := x.(type) {
	case *Literal:
		return v.Value
	case *Parameter:
		return p[v.Name]
	}
	return nil
}
func uintValue(v any) (uint32, bool) {
	switch n := v.(type) {
	case int:
		return uint32(n), n >= 0
	case int64:
		return uint32(n), n >= 0 && n <= math.MaxUint32
	case uint32:
		return n, true
	case uint64:
		return uint32(n), n <= math.MaxUint32
	case float64:
		return uint32(n), n >= 0 && n <= math.MaxUint32 && n == math.Trunc(n)
	case string:
		x, e := strconv.ParseUint(n, 10, 32)
		return uint32(x), e == nil
	}
	return 0, false
}

func requiredFields(q *Select, alias string, info TableInfo) []string {
	needed := map[string]bool{}
	var walk func(Expr)
	walk = func(x Expr) {
		switch v := x.(type) {
		case *Identifier:
			if v.Qualifier == "" || strings.EqualFold(v.Qualifier, alias) {
				needed[strings.ToLower(v.Name)] = true
			}
		case *Star:
			if v.Qualifier == "" || strings.EqualFold(v.Qualifier, alias) {
				for _, c := range info.Columns {
					needed[strings.ToLower(c.Name)] = true
				}
			}
		case *UnaryExpr:
			walk(v.X)
		case *BinaryExpr:
			walk(v.Left)
			walk(v.Right)
		case *InExpr:
			walk(v.X)
			for _, a := range v.List {
				walk(a)
			}
		case *BetweenExpr:
			walk(v.X)
			walk(v.Low)
			walk(v.High)
		case *IsNullExpr:
			walk(v.X)
		case *CallExpr:
			for _, a := range v.Args {
				walk(a)
			}
		case *CaseExpr:
			walk(v.Base)
			for _, w := range v.Whens {
				walk(w.When)
				walk(w.Then)
			}
			walk(v.Else)
		case *CastExpr:
			walk(v.X)
		}
	}
	for _, it := range q.Items {
		walk(it.Expr)
	}
	walk(q.Where)
	walk(q.Having)
	for _, j := range q.Joins {
		walk(j.On)
	}
	for _, x := range q.GroupBy {
		walk(x)
	}
	for _, o := range q.OrderBy {
		walk(o.Expr)
	}
	out := make([]string, 0, len(needed))
	for _, c := range info.Columns {
		if needed[strings.ToLower(c.Name)] {
			out = append(out, c.Name)
		}
	}
	return out
}

type joinStats struct {
	physical    string
	buildSide   string
	probeSide   string
	hashEntries int
	peakBytes   int64
	fallback    string
}

func (e *execEnv) joinRows(left, right []Row, j Join, limit int) ([]Row, joinStats, error) {
	if j.Type == JoinCross {
		if err := e.guardNestedJoin(len(left), len(right), j.Pos); err != nil {
			return nil, joinStats{}, err
		}
		capacity := len(left) * len(right)
		if limit > 0 && limit < capacity {
			capacity = limit
		}
		out := make([]Row, 0, capacity)
		for _, l := range left {
			for _, r := range right {
				out = append(out, mergeRows(l, r))
				if limit > 0 && len(out) >= limit {
					return out, joinStats{physical: "BlockNestedLoopJoin", buildSide: "right", probeSide: "left", peakBytes: int64(len(out)) * 96}, nil
				}
			}
		}
		return out, joinStats{physical: "BlockNestedLoopJoin", buildSide: "right", probeSide: "left", peakBytes: int64(len(out)) * 96}, nil
	}
	bl, ok := j.On.(*BinaryExpr)
	if !ok || bl.Op != "=" {
		return e.nestedJoin(left, right, j, limit)
	}
	li, lok := bl.Left.(*Identifier)
	ri, rok := bl.Right.(*Identifier)
	if !lok || !rok {
		return e.nestedJoin(left, right, j, limit)
	}
	leftExpr, rightExpr := Expr(li), Expr(ri)
	if len(left) > 0 && len(right) > 0 && exprPresent(ri, left[0]) && exprPresent(li, right[0]) {
		leftExpr, rightExpr = ri, li
	}
	buildLeft := j.Type == JoinInner && len(left) < len(right)
	buildRows, probeRows := right, left
	buildExpr, probeExpr := rightExpr, leftExpr
	stats := joinStats{physical: "TypedHashJoin", buildSide: "right", probeSide: "left"}
	if buildLeft {
		buildRows, probeRows = left, right
		buildExpr, probeExpr = leftExpr, rightExpr
		stats.buildSide, stats.probeSide = "left", "right"
	}
	if err := e.requireMemory(int64(len(buildRows))*112, j.Pos, "hash join"); err != nil {
		return nil, stats, err
	}
	type indexed struct {
		index int
		row   Row
	}
	h := make(map[joinHashKey][]indexed, len(buildRows))
	for i, r := range buildRows {
		value := e.eval(buildExpr, r, nil, nil)
		if value == nil {
			continue
		}
		k := makeJoinHashKey(value)
		h[k] = append(h[k], indexed{i, r})
	}
	stats.hashEntries = len(buildRows)
	stats.peakBytes = int64(len(buildRows)) * 112
	if !buildLeft {
		capacity := 0
		if limit > 0 {
			capacity = limit
		}
		out := make([]Row, 0, capacity)
		for _, l := range probeRows {
			value := e.eval(probeExpr, l, nil, nil)
			matches := h[makeJoinHashKey(value)]
			if len(matches) == 0 && j.Type == JoinLeft {
				out = append(out, mergeRows(l, nil))
				if limit > 0 && len(out) >= limit {
					return out, stats, nil
				}
				continue
			}
			matched := false
			for _, hit := range matches {
				m := mergeRows(l, hit.row)
				okv, err := truth(e.eval(j.On, m, nil, nil))
				if err != nil {
					return nil, stats, err
				}
				if okv != nil && *okv {
					out = append(out, m)
					matched = true
					if limit > 0 && len(out) >= limit {
						return out, stats, nil
					}
				}
			}
			if !matched && j.Type == JoinLeft {
				out = append(out, mergeRows(l, nil))
				if limit > 0 && len(out) >= limit {
					return out, stats, nil
				}
			}
		}
		return out, stats, nil
	}
	// Building the left side is cheaper for an inner join. Accumulate by the
	// original left index so output remains deterministic and left-major.
	byLeft := make([][]Row, len(left))
	for _, r := range probeRows {
		value := e.eval(probeExpr, r, nil, nil)
		for _, hit := range h[makeJoinHashKey(value)] {
			m := mergeRows(hit.row, r)
			okv, err := truth(e.eval(j.On, m, nil, nil))
			if err != nil {
				return nil, stats, err
			}
			if okv != nil && *okv {
				byLeft[hit.index] = append(byLeft[hit.index], m)
			}
		}
	}
	out := make([]Row, 0)
	for _, matches := range byLeft {
		if limit > 0 && len(out)+len(matches) > limit {
			matches = matches[:limit-len(out)]
		}
		out = append(out, matches...)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, stats, nil
}

func exprPresent(id *Identifier, row Row) bool {
	if id == nil {
		return false
	}
	want := id.Name
	if id.Qualifier != "" {
		want = id.Qualifier + "." + id.Name
	}
	for key := range row {
		if strings.EqualFold(key, want) {
			return true
		}
	}
	return false
}
func (e *execEnv) nestedJoin(left, right []Row, j Join, limit int) ([]Row, joinStats, error) {
	if err := e.guardNestedJoin(len(left), len(right), j.Pos); err != nil {
		return nil, joinStats{}, err
	}
	capacity := 0
	if limit > 0 {
		capacity = limit
	}
	out := make([]Row, 0, capacity)
	for _, l := range left {
		matched := false
		for _, r := range right {
			m := mergeRows(l, r)
			ok, err := truth(e.eval(j.On, m, nil, nil))
			if err != nil {
				return nil, joinStats{}, err
			}
			if ok != nil && *ok {
				out = append(out, m)
				matched = true
				if limit > 0 && len(out) >= limit {
					return out, joinStats{physical: "BlockNestedLoopJoin", buildSide: "right", probeSide: "left", peakBytes: int64(len(out)) * 96, fallback: "non-equality predicate"}, nil
				}
			}
		}
		if !matched && j.Type == JoinLeft {
			out = append(out, l)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, joinStats{physical: "BlockNestedLoopJoin", buildSide: "right", probeSide: "left", peakBytes: int64(len(out)) * 96, fallback: "non-equality predicate"}, nil
}

func (e *execEnv) guardNestedJoin(left, right int, pos Position) error {
	pairs := int64(left) * int64(right)
	if pairs < 0 || pairs > e.memoryLimit/96 {
		return sqlErr("sql_memory_limit", pos, fmt.Sprintf("nested join requires %d candidate pairs, memory budget is %d bytes", pairs, e.memoryLimit))
	}
	return nil
}

func (e *execEnv) project(q *Select, rows []Row, outer Row) ([]map[string]any, error) {
	started := time.Now()
	if len(q.Items) == 1 {
		if star, ok := q.Items[0].Expr.(*Star); ok && star.Qualifier == "" && rowsAreUnqualified(rows) {
			out := make([]map[string]any, len(rows))
			for i, row := range rows {
				out[i] = map[string]any(row)
			}
			e.addOp(OperatorStats{Name: "project", Physical: "IdentityProjection", InputRows: len(rows), OutputRows: len(out), BatchCount: maxInt(1, (len(rows)+255)/256), PeakBytes: int64(len(out)) * 8, Duration: time.Since(started)})
			return out, nil
		}
	}
	out := make([]map[string]any, 0, len(rows))
	batches := 0
	for base := 0; base < len(rows); base += 256 {
		end := minInt(base+256, len(rows))
		batches++
		for _, r := range rows[base:end] {
			dst := map[string]any{}
			for i, it := range q.Items {
				if s, ok := it.Expr.(*Star); ok {
					for k, v := range r {
						if !strings.Contains(k, ".") {
							if s.Qualifier == "" {
								dst[k] = v
							}
						} else if s.Qualifier == "" || strings.HasPrefix(strings.ToLower(k), strings.ToLower(s.Qualifier)+".") {
							name := k[strings.IndexByte(k, '.')+1:]
							dst[name] = v
						}
					}
					continue
				}
				name := it.Alias
				if name == "" {
					name = exprName(it.Expr, i)
				}
				dst[name] = e.eval(it.Expr, r, outer, nil)
				if e.evalErr != nil {
					err := e.evalErr
					e.evalErr = nil
					return nil, err
				}
			}
			out = append(out, dst)
		}
	}
	e.addOp(OperatorStats{Name: "project", Physical: "TypedColumnBatchProject", InputRows: len(rows), OutputRows: len(out), BatchCount: batches, PeakBytes: int64(minInt(len(rows), 256)) * 96, Duration: time.Since(started)})
	return out, nil
}

func rowsAreUnqualified(rows []Row) bool {
	if len(rows) == 0 {
		return true
	}
	for key := range rows[0] {
		if strings.Contains(key, ".") {
			return false
		}
	}
	return true
}

func (e *execEnv) projectOne(q *Select, r Row, outer Row) (map[string]any, error) {
	dst := map[string]any{}
	for i, it := range q.Items {
		if s, ok := it.Expr.(*Star); ok {
			for k, v := range r {
				if !strings.Contains(k, ".") {
					if s.Qualifier == "" {
						dst[k] = v
					}
				} else if s.Qualifier == "" || strings.HasPrefix(strings.ToLower(k), strings.ToLower(s.Qualifier)+".") {
					name := k[strings.IndexByte(k, '.')+1:]
					dst[name] = v
				}
			}
			continue
		}
		name := it.Alias
		if name == "" {
			name = exprName(it.Expr, i)
		}
		dst[name] = e.eval(it.Expr, r, outer, nil)
		if e.evalErr != nil {
			err := e.evalErr
			e.evalErr = nil
			return nil, err
		}
	}
	return dst, nil
}

type aggregateState struct{ rows []Row }

func (e *execEnv) aggregate(q *Select, rows []Row, outer Row) ([]map[string]any, error) {
	started := time.Now()
	groups := map[string]*aggregateState{}
	order := []string{}
	if len(q.GroupBy) == 0 {
		groups[""] = &aggregateState{rows: rows}
		order = []string{""}
	} else {
		for _, r := range rows {
			vals := make([]any, len(q.GroupBy))
			for i, x := range q.GroupBy {
				vals[i] = e.eval(x, r, outer, nil)
				if e.evalErr != nil {
					err := e.evalErr
					e.evalErr = nil
					return nil, err
				}
			}
			k := hashKey(vals)
			if groups[k] == nil {
				if err := e.requireMemory(int64(len(groups)+1)*160, q.Pos, "aggregate"); err != nil {
					return nil, err
				}
				groups[k] = &aggregateState{}
				order = append(order, k)
			}
			groups[k].rows = append(groups[k].rows, r)
		}
	}
	out := make([]map[string]any, 0, len(groups))
	for _, k := range order {
		g := groups[k]
		base := Row{}
		if len(g.rows) > 0 {
			base = g.rows[0]
		}
		dst := map[string]any{}
		for i, it := range q.Items {
			name := it.Alias
			if name == "" {
				name = exprName(it.Expr, i)
			}
			dst[name] = e.eval(it.Expr, base, outer, g.rows)
			if e.evalErr != nil {
				err := e.evalErr
				e.evalErr = nil
				return nil, err
			}
		}
		if q.Having != nil {
			value := e.eval(q.Having, base, outer, g.rows)
			if e.evalErr != nil {
				err := e.evalErr
				e.evalErr = nil
				return nil, err
			}
			ok, err := truth(value)
			if err != nil {
				return nil, err
			}
			if ok == nil || !*ok {
				continue
			}
		}
		out = append(out, dst)
	}
	e.addOp(OperatorStats{Name: "aggregate", Physical: "TypedHashAggregate", InputRows: len(rows), OutputRows: len(out), HashEntries: len(groups), PeakBytes: int64(len(groups)) * 160, Duration: time.Since(started)})
	return out, nil
}

func hasAggregate(q *Select) bool {
	var found bool
	var walk func(Expr)
	walk = func(x Expr) {
		switch v := x.(type) {
		case *CallExpr:
			if isAggregate(v.Name) {
				found = true
			}
			for _, a := range v.Args {
				walk(a)
			}
		case *UnaryExpr:
			walk(v.X)
		case *BinaryExpr:
			walk(v.Left)
			walk(v.Right)
		case *CaseExpr:
			for _, w := range v.Whens {
				walk(w.When)
				walk(w.Then)
			}
			walk(v.Else)
		}
	}
	for _, it := range q.Items {
		walk(it.Expr)
	}
	walk(q.Having)
	return found
}
func isAggregate(n string) bool {
	switch strings.ToUpper(n) {
	case "COUNT", "MIN", "MAX", "SUM", "AVG":
		return true
	}
	return false
}

func (e *execEnv) eval(x Expr, row, outer Row, group []Row) any {
	switch v := x.(type) {
	case nil:
		return nil
	case *Literal:
		return v.Value
	case *Parameter:
		return e.params[v.Name]
	case *Identifier:
		return lookupValue(row, outer, v)
	case *UnaryExpr:
		a := e.eval(v.X, row, outer, group)
		switch strings.ToUpper(v.Op) {
		case "NOT":
			b, _ := truth(a)
			if b == nil {
				return nil
			}
			return !*b
		case "-":
			n, ok := number(a)
			if !ok {
				return nil
			}
			return -n
		case "+":
			n, ok := number(a)
			if !ok {
				return nil
			}
			return n
		}
	case *BinaryExpr:
		return evalBinary(v.Op, e.eval(v.Left, row, outer, group), e.eval(v.Right, row, outer, group))
	case *InExpr:
		a := e.eval(v.X, row, outer, group)
		if a == nil {
			return nil
		}
		list := v.List
		if v.Query != nil {
			rr, err := e.runSelect(v.Query, row)
			if err != nil {
				e.evalErr = err
				return nil
			}
			cols := projectionNames(v.Query, e)
			for _, resultRow := range rr {
				var value any
				if len(cols) > 0 {
					value = resultRow[cols[0]]
				} else {
					keys := make([]string, 0, len(resultRow))
					for k := range resultRow {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					if len(keys) > 0 {
						value = resultRow[keys[0]]
					}
				}
				list = append(list, &Literal{Value: value})
			}
		}
		found := false
		null := false
		for _, z := range list {
			b := e.eval(z, row, outer, group)
			if b == nil {
				null = true
			} else if compare(a, b) == 0 {
				found = true
				break
			}
		}
		if !found && null {
			return nil
		}
		if v.Not {
			return !found
		}
		return found
	case *BetweenExpr:
		a := e.eval(v.X, row, outer, group)
		lo := e.eval(v.Low, row, outer, group)
		hi := e.eval(v.High, row, outer, group)
		if a == nil || lo == nil || hi == nil {
			return nil
		}
		ok := compare(a, lo) >= 0 && compare(a, hi) <= 0
		if v.Not {
			return !ok
		}
		return ok
	case *IsNullExpr:
		ok := e.eval(v.X, row, outer, group) == nil
		if v.Not {
			return !ok
		}
		return ok
	case *CallExpr:
		return e.evalCall(v, row, outer, group)
	case *CaseExpr:
		for _, w := range v.Whens {
			ok, _ := truth(e.eval(w.When, row, outer, group))
			if ok != nil && *ok {
				return e.eval(w.Then, row, outer, group)
			}
		}
		return e.eval(v.Else, row, outer, group)
	case *CastExpr:
		return castValue(e.eval(v.X, row, outer, group), v.Type)
	case *ExistsExpr:
		rr, err := e.runSelect(v.Query, row)
		if err != nil {
			e.evalErr = err
			return nil
		}
		ok := len(rr) > 0
		if v.Not {
			return !ok
		}
		return ok
	}
	return nil
}

func (e *execEnv) evalCall(c *CallExpr, row, outer Row, group []Row) any {
	n := strings.ToUpper(c.Name)
	if isAggregate(n) {
		vals := []any{}
		for _, r := range group {
			if len(c.Args) == 0 {
				vals = append(vals, int64(1))
				continue
			}
			if _, ok := c.Args[0].(*Star); ok {
				vals = append(vals, int64(1))
				continue
			}
			v := e.eval(c.Args[0], r, outer, nil)
			if v != nil {
				vals = append(vals, v)
			}
		}
		if c.Distinct {
			vals = distinctValues(vals)
		}
		switch n {
		case "COUNT":
			return int64(len(vals))
		case "MIN":
			if len(vals) == 0 {
				return nil
			}
			m := vals[0]
			for _, v := range vals[1:] {
				if compare(v, m) < 0 {
					m = v
				}
			}
			return m
		case "MAX":
			if len(vals) == 0 {
				return nil
			}
			m := vals[0]
			for _, v := range vals[1:] {
				if compare(v, m) > 0 {
					m = v
				}
			}
			return m
		case "SUM", "AVG":
			if len(vals) == 0 {
				return nil
			}
			sum := float64(0)
			for _, v := range vals {
				x, ok := number(v)
				if ok {
					sum += x
				}
			}
			if n == "AVG" {
				return sum / float64(len(vals))
			}
			return sum
		}
	}
	args := make([]any, len(c.Args))
	for i, a := range c.Args {
		args[i] = e.eval(a, row, outer, group)
	}
	switch n {
	case "LOWER":
		if len(args) == 1 && args[0] != nil {
			return strings.ToLower(fmt.Sprint(args[0]))
		}
	case "UPPER":
		if len(args) == 1 && args[0] != nil {
			return strings.ToUpper(fmt.Sprint(args[0]))
		}
	case "LENGTH":
		if len(args) == 1 && args[0] != nil {
			return int64(len([]rune(fmt.Sprint(args[0]))))
		}
	case "COALESCE":
		for _, a := range args {
			if a != nil {
				return a
			}
		}
	case "NULLIF":
		if len(args) == 2 && compare(args[0], args[1]) == 0 {
			return nil
		}
		if len(args) > 0 {
			return args[0]
		}
	}
	return nil
}

func evalBinary(op string, a, b any) any {
	u := strings.ToUpper(op)
	if u == "AND" || u == "OR" {
		aa, _ := truth(a)
		bb, _ := truth(b)
		if u == "AND" {
			if aa != nil && !*aa {
				return false
			}
			if bb != nil && !*bb {
				return false
			}
			if aa == nil || bb == nil {
				return nil
			}
			return true
		}
		if aa != nil && *aa {
			return true
		}
		if bb != nil && *bb {
			return true
		}
		if aa == nil || bb == nil {
			return nil
		}
		return false
	}
	if a == nil || b == nil {
		return nil
	}
	switch u {
	case "=":
		return compare(a, b) == 0
	case "!=", "<>":
		return compare(a, b) != 0
	case "<":
		return compare(a, b) < 0
	case "<=":
		return compare(a, b) <= 0
	case ">":
		return compare(a, b) > 0
	case ">=":
		return compare(a, b) >= 0
	case "LIKE":
		return like(fmt.Sprint(a), fmt.Sprint(b))
	case "+", "-", "*", "/", "%":
		x, xok := number(a)
		y, yok := number(b)
		if !xok || !yok {
			return nil
		}
		switch u {
		case "+":
			return x + y
		case "-":
			return x - y
		case "*":
			return x * y
		case "/":
			if y == 0 {
				return nil
			}
			return x / y
		case "%":
			if y == 0 {
				return nil
			}
			return math.Mod(x, y)
		}
	}
	return nil
}
func truth(v any) (*bool, error) {
	if v == nil {
		return nil, nil
	}
	if b, ok := v.(bool); ok {
		return &b, nil
	}
	return nil, sqlErr("sql_type_mismatch", Position{}, "boolean expression required")
}
func number(v any) (float64, bool) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return 0, false
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}
func compare(a, b any) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}
	if x, ok := number(a); ok {
		if y, yok := number(b); yok {
			if x < y {
				return -1
			}
			if x > y {
				return 1
			}
			return 0
		}
	}
	as, bs := fmt.Sprint(a), fmt.Sprint(b)
	return strings.Compare(as, bs)
}
func like(s, p string) bool {
	parts := strings.Split(p, "%")
	if len(parts) == 1 {
		return s == p
	}
	pos := 0
	for i, x := range parts {
		if x == "" {
			continue
		}
		j := strings.Index(s[pos:], x)
		if j < 0 {
			return false
		}
		if i == 0 && !strings.HasPrefix(p, "%") && j != 0 {
			return false
		}
		pos += j + len(x)
	}
	return strings.HasSuffix(p, "%") || strings.HasSuffix(s, parts[len(parts)-1])
}
func castValue(v any, t string) any {
	if v == nil {
		return nil
	}
	switch strings.ToUpper(t) {
	case "STRING", "TEXT", "VARCHAR":
		return fmt.Sprint(v)
	case "INT", "INTEGER", "BIGINT":
		n, ok := number(v)
		if ok {
			return int64(n)
		}
		x, e := strconv.ParseInt(fmt.Sprint(v), 10, 64)
		if e == nil {
			return x
		}
	case "FLOAT", "DOUBLE", "REAL":
		n, ok := number(v)
		if ok {
			return n
		}
		x, e := strconv.ParseFloat(fmt.Sprint(v), 64)
		if e == nil {
			return x
		}
	case "BOOL", "BOOLEAN":
		x, e := strconv.ParseBool(fmt.Sprint(v))
		if e == nil {
			return x
		}
	}
	return nil
}

func lookupValue(row, outer Row, id *Identifier) any {
	key := id.Name
	if id.Qualifier != "" {
		key = id.Qualifier + "." + id.Name
	}
	for k, v := range row {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	for k, v := range outer {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	if id.Qualifier == "" {
		var found any
		count := 0
		for k, v := range row {
			if idx := strings.LastIndexByte(k, '.'); idx >= 0 && strings.EqualFold(k[idx+1:], id.Name) {
				found = v
				count++
			}
		}
		if count == 1 {
			return found
		}
	}
	return nil
}
func qualify(r Row, alias string) Row {
	out := make(Row, len(r)*2)
	for k, v := range r {
		out[alias+"."+k] = v
	}
	return out
}
func qualifyRows(rows []map[string]any, alias string) []Row {
	out := make([]Row, len(rows))
	for i, r := range rows {
		out[i] = qualify(r, alias)
	}
	return out
}
func mergeRows(a, b Row) Row {
	out := make(Row, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
func hashKey(v any) string { b, _ := json.Marshal(v); return string(b) }

type joinHashKey struct {
	kind   byte
	number uint64
	text   string
	flag   bool
}

func makeJoinHashKey(v any) joinHashKey {
	if n, ok := number(v); ok {
		if n == 0 {
			n = 0 // normalize negative zero
		}
		bits := math.Float64bits(n)
		if math.IsNaN(n) {
			bits = 0x7ff8000000000000
		}
		return joinHashKey{kind: 'n', number: bits}
	}
	switch x := v.(type) {
	case nil:
		return joinHashKey{kind: '0'}
	case string:
		return joinHashKey{kind: 's', text: x}
	case bool:
		return joinHashKey{kind: 'b', flag: x}
	default:
		return joinHashKey{kind: 'j', text: hashKey(v)}
	}
}
func distinctValues(in []any) []any {
	seen := map[string]bool{}
	out := in[:0]
	for _, v := range in {
		k := hashKey(v)
		if !seen[k] {
			seen[k] = true
			out = append(out, v)
		}
	}
	return out
}
func distinctRows(in []map[string]any) []map[string]any {
	seen := map[string]bool{}
	out := in[:0]
	for _, r := range in {
		k := hashKey(r)
		if !seen[k] {
			seen[k] = true
			out = append(out, r)
		}
	}
	return out
}

func (e *execEnv) distinctRows(in []map[string]any, pos Position) ([]map[string]any, int64, error) {
	seen := make(map[string]struct{}, minInt(len(in), 1024))
	out := in[:0]
	for _, r := range in {
		k := hashKey(r)
		if _, ok := seen[k]; ok {
			continue
		}
		if err := e.requireMemory(int64(len(seen)+1)*128, pos, "distinct"); err != nil {
			return nil, int64(len(seen)) * 128, err
		}
		seen[k] = struct{}{}
		out = append(out, r)
	}
	return out, int64(len(seen)) * 128, nil
}

type topKItem struct {
	row   map[string]any
	index int
	key   any
}

type topKHeap struct {
	env   *execEnv
	terms []OrderTerm
	items []topKItem
}

func (h topKHeap) Len() int { return len(h.items) }
func (h topKHeap) Less(i, j int) bool {
	// container/heap puts the minimum at root; define "minimum" as the
	// worst result so replacement is O(log k). Later equal rows are worse.
	if len(h.terms) == 1 {
		cmp := compareTopKKey(h.terms[0], h.items[i].key, h.items[j].key)
		if cmp == 0 {
			return h.items[i].index > h.items[j].index
		}
		return cmp > 0
	}
	if h.env.lessOrder(h.terms, h.items[j].row, h.items[i].row) {
		return true
	}
	if h.env.lessOrder(h.terms, h.items[i].row, h.items[j].row) {
		return false
	}
	return h.items[i].index > h.items[j].index
}
func (h topKHeap) Swap(i, j int) { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *topKHeap) Push(x any)   { h.items = append(h.items, x.(topKItem)) }
func (h *topKHeap) Pop() any {
	n := len(h.items)
	x := h.items[n-1]
	h.items = h.items[:n-1]
	return x
}

func (e *execEnv) topK(rows []map[string]any, terms []OrderTerm, k int, pos Position) ([]map[string]any, error) {
	if k <= 0 || len(rows) == 0 {
		return nil, nil
	}
	if k >= len(rows) {
		if err := e.requireMemory(int64(len(rows))*96, pos, "top-k"); err != nil {
			return nil, err
		}
		sort.SliceStable(rows, func(i, j int) bool { return e.lessOrder(terms, rows[i], rows[j]) })
		return rows, nil
	}
	if err := e.requireMemory(int64(k)*112, pos, "top-k"); err != nil {
		return nil, err
	}
	h := &topKHeap{env: e, terms: terms, items: make([]topKItem, 0, k)}
	for i, row := range rows {
		item := topKItem{row: row, index: i}
		if len(terms) == 1 {
			item.key = e.eval(terms[0].Expr, row, nil, nil)
			if e.evalErr != nil {
				err := e.evalErr
				e.evalErr = nil
				return nil, err
			}
		}
		if h.Len() < k {
			heap.Push(h, item)
			continue
		}
		worst := h.items[0]
		better := false
		if len(terms) == 1 {
			cmp := compareTopKKey(terms[0], item.key, worst.key)
			better = cmp < 0 || (cmp == 0 && item.index < worst.index)
		} else {
			better = e.lessOrder(terms, item.row, worst.row)
			if !better && !e.lessOrder(terms, worst.row, item.row) {
				better = item.index < worst.index
			}
		}
		if better {
			h.items[0] = item
			heap.Fix(h, 0)
		}
	}
	sort.SliceStable(h.items, func(i, j int) bool {
		if len(terms) == 1 {
			cmp := compareTopKKey(terms[0], h.items[i].key, h.items[j].key)
			if cmp == 0 {
				return h.items[i].index < h.items[j].index
			}
			return cmp < 0
		}
		if e.lessOrder(terms, h.items[i].row, h.items[j].row) {
			return true
		}
		if e.lessOrder(terms, h.items[j].row, h.items[i].row) {
			return false
		}
		return h.items[i].index < h.items[j].index
	})
	out := make([]map[string]any, len(h.items))
	for i := range h.items {
		out[i] = h.items[i].row
	}
	return out, nil
}

func compareTopKKey(term OrderTerm, a, b any) int {
	cmp := compare(a, b)
	if term.Desc {
		return -cmp
	}
	return cmp
}

func (e *execEnv) requireMemory(bytes int64, pos Position, operator string) error {
	if bytes > e.peakBytes {
		e.peakBytes = bytes
		e.metrics.PeakBytes = bytes
	}
	if e.memoryLimit > 0 && bytes > e.memoryLimit {
		return sqlErr("sql_memory_limit", pos, fmt.Sprintf("%s requires approximately %d bytes, query budget is %d bytes", operator, bytes, e.memoryLimit))
	}
	return nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func (e *execEnv) lessOrder(terms []OrderTerm, a, b map[string]any) bool {
	for _, t := range terms {
		av := e.eval(t.Expr, a, nil, nil)
		bv := e.eval(t.Expr, b, nil, nil)
		c := compare(av, bv)
		if c == 0 {
			continue
		}
		if av == nil || bv == nil {
			first := !t.Desc
			if t.NullsFirst != nil {
				first = *t.NullsFirst
			}
			if av == nil {
				return first
			}
			return !first
		}
		if t.Desc {
			return c > 0
		}
		return c < 0
	}
	return false
}
func exprName(x Expr, i int) string {
	switch v := x.(type) {
	case *Identifier:
		return v.Name
	case *CallExpr:
		return strings.ToLower(v.Name)
	case *CastExpr:
		return exprName(v.X, i)
	}
	return fmt.Sprintf("column_%d", i+1)
}
func projectionNames(q *Select, e *execEnv) []string {
	var out []string
	for i, it := range q.Items {
		if s, ok := it.Expr.(*Star); ok {
			for alias, info := range e.infos {
				if s.Qualifier == "" || strings.EqualFold(s.Qualifier, alias) {
					for _, c := range info.Columns {
						out = append(out, c.Name)
					}
				}
			}
			continue
		}
		if it.Alias != "" {
			out = append(out, it.Alias)
		} else {
			out = append(out, exprName(it.Expr, i))
		}
	}
	return out
}
func (e *execEnv) addOp(op OperatorStats) {
	e.plan.Operators = append(e.plan.Operators, op)
	e.plan.Physical = append(e.plan.Physical, op.Physical)
	e.metrics.InputRows += op.InputRows
	e.metrics.VisitedRows += op.VisitedRows
	e.metrics.ScannedRows += op.ScannedRows
	e.metrics.DecodedRows += op.DecodedRows
	e.metrics.DecodedFields += op.DecodedFields
	e.metrics.ReadBytes += op.ReadBytes
	e.metrics.SpillBytes += op.SpillBytes
	if op.PeakBytes > e.metrics.PeakBytes {
		e.metrics.PeakBytes = op.PeakBytes
	}
}
