package sqlquery

import (
	"fmt"
	"strconv"
	"strings"
)

type parser struct {
	tokens []token
	i      int
}

func Parse(src string) (*Statement, error) {
	t, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: t}
	st := &Statement{}
	if p.keyword("EXPLAIN") {
		st.Explain = true
		st.Analyze = p.keyword("ANALYZE")
	}
	q, err := p.parseSelect()
	if err != nil {
		return nil, err
	}
	st.Query = q
	p.match(tokSemicolon)
	if p.peek().kind != tokEOF {
		return nil, p.err(p.peek(), "unexpected token "+p.peek().lit)
	}
	return st, nil
}

func ExtractTableNames(src string) ([]string, error) {
	st, err := Parse(src)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	ctes := map[string]bool{}
	var out []string
	var walk func(*Select)
	walk = func(q *Select) {
		for _, c := range q.CTEs {
			ctes[strings.ToLower(c.Name)] = true
			walk(c.Query)
		}
		add := func(t TableRef) {
			if t.Subquery != nil {
				walk(t.Subquery)
				return
			}
			if (t.Catalog == "" || strings.EqualFold(t.Catalog, "static")) && !ctes[strings.ToLower(t.Name)] {
				if t.Name != "" && !seen[strings.ToLower(t.Name)] {
					seen[strings.ToLower(t.Name)] = true
					out = append(out, t.Name)
				}
			}
		}
		add(q.From)
		for _, j := range q.Joins {
			add(j.Table)
		}
		if q.UnionAll != nil {
			walk(q.UnionAll)
		}
	}
	walk(st.Query)
	return out, nil
}

func (p *parser) parseSelect() (*Select, error) {
	q := &Select{Pos: p.peek().pos}
	if p.keyword("WITH") {
		q.Recursive = p.keyword("RECURSIVE")
		for {
			name, err := p.ident()
			if err != nil {
				return nil, err
			}
			if !p.keyword("AS") {
				return nil, p.err(p.peek(), "expected AS after CTE name")
			}
			if !p.match(tokLParen) {
				return nil, p.err(p.peek(), "expected '(' before CTE query")
			}
			cq, err := p.parseSelect()
			if err != nil {
				return nil, err
			}
			if !p.match(tokRParen) {
				return nil, p.err(p.peek(), "expected ')' after CTE query")
			}
			q.CTEs = append(q.CTEs, CTE{Name: name, Query: cq})
			if !p.match(tokComma) {
				break
			}
		}
	}
	if !p.keyword("SELECT") {
		return nil, p.err(p.peek(), "expected SELECT")
	}
	q.Distinct = p.keyword("DISTINCT")
	for {
		e, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		item := SelectItem{Expr: e}
		if p.keyword("AS") {
			item.Alias, err = p.ident()
			if err != nil {
				return nil, err
			}
		} else if p.peek().kind == tokIdent && !isClauseKeyword(p.peek().lit) {
			item.Alias = p.next().lit
		}
		q.Items = append(q.Items, item)
		if !p.match(tokComma) {
			break
		}
	}
	if !p.keyword("FROM") {
		return nil, p.err(p.peek(), "expected FROM")
	}
	var err error
	q.From, err = p.parseTable()
	if err != nil {
		return nil, err
	}
	for {
		jt := JoinInner
		pos := p.peek().pos
		switch {
		case p.keywordPeek("RIGHT"), p.keywordPeek("FULL"):
			return nil, sqlErr("sql_unsupported_feature", p.peek().pos, strings.ToUpper(p.peek().lit)+" JOIN is not supported")
		case p.keyword("LEFT"):
			jt = JoinLeft
			if !p.keyword("JOIN") {
				return nil, p.err(p.peek(), "expected JOIN after LEFT")
			}
		case p.keyword("INNER"):
			if !p.keyword("JOIN") {
				return nil, p.err(p.peek(), "expected JOIN after INNER")
			}
		case p.keyword("CROSS"):
			jt = JoinCross
			if !p.keyword("JOIN") {
				return nil, p.err(p.peek(), "expected JOIN after CROSS")
			}
		case p.keyword("JOIN"):
		default:
			goto joinsDone
		}
		t, err := p.parseTable()
		if err != nil {
			return nil, err
		}
		j := Join{Pos: pos, Type: jt, Table: t}
		if jt != JoinCross {
			if !p.keyword("ON") {
				return nil, p.err(p.peek(), "expected ON")
			}
			j.On, err = p.parseExpr(0)
			if err != nil {
				return nil, err
			}
		}
		q.Joins = append(q.Joins, j)
	}
joinsDone:
	if p.keyword("WHERE") {
		q.Where, err = p.parseExpr(0)
		if err != nil {
			return nil, err
		}
	}
	if p.keyword("GROUP") {
		if !p.keyword("BY") {
			return nil, p.err(p.peek(), "expected BY")
		}
		q.GroupBy, err = p.exprList()
		if err != nil {
			return nil, err
		}
	}
	if p.keyword("HAVING") {
		q.Having, err = p.parseExpr(0)
		if err != nil {
			return nil, err
		}
	}
	if p.keyword("ORDER") {
		if !p.keyword("BY") {
			return nil, p.err(p.peek(), "expected BY")
		}
		for {
			e, er := p.parseExpr(0)
			if er != nil {
				return nil, er
			}
			o := OrderTerm{Expr: e}
			if p.keyword("DESC") {
				o.Desc = true
			} else {
				p.keyword("ASC")
			}
			if p.keyword("NULLS") {
				v := true
				if p.keyword("LAST") {
					v = false
				} else if !p.keyword("FIRST") {
					return nil, p.err(p.peek(), "expected FIRST or LAST")
				}
				o.NullsFirst = &v
			}
			q.OrderBy = append(q.OrderBy, o)
			if !p.match(tokComma) {
				break
			}
		}
	}
	if p.keyword("LIMIT") {
		n, er := p.intLiteral()
		if er != nil {
			return nil, er
		}
		q.Limit = &n
	}
	if p.keyword("OFFSET") {
		q.Offset, err = p.intLiteral()
		if err != nil {
			return nil, err
		}
	}
	if p.keyword("UNION") {
		if !p.keyword("ALL") {
			return nil, sqlErr("sql_unsupported_feature", p.peek().pos, "only UNION ALL is supported")
		}
		right, er := p.parseSelect()
		if er != nil {
			return nil, er
		}
		q.UnionAll = right
	}
	return q, nil
}

func (p *parser) parseTable() (TableRef, error) {
	t := TableRef{Pos: p.peek().pos}
	if p.match(tokLParen) {
		q, err := p.parseSelect()
		if err != nil {
			return t, err
		}
		if !p.match(tokRParen) {
			return t, p.err(p.peek(), "expected ')' after derived table")
		}
		t.Subquery = q
	} else {
		name, err := p.ident()
		if err != nil {
			return t, err
		}
		if p.match(tokDot) {
			t.Catalog = name
			t.Name, err = p.ident()
			if err != nil {
				return t, err
			}
		} else {
			t.Name = name
		}
	}
	if p.keyword("AS") {
		a, err := p.ident()
		if err != nil {
			return t, err
		}
		t.Alias = a
	} else if p.peek().kind == tokIdent && !isClauseKeyword(p.peek().lit) {
		t.Alias = p.next().lit
	}
	if t.Subquery != nil && t.Alias == "" {
		return t, p.err(t.PosToken(), "derived table requires alias")
	}
	return t, nil
}

func (t TableRef) PosToken() token { return token{pos: t.Pos} }

func (p *parser) exprList() ([]Expr, error) {
	var out []Expr
	for {
		e, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
		if !p.match(tokComma) {
			return out, nil
		}
	}
}

var precedence = map[string]int{"OR": 1, "AND": 2, "=": 3, "!=": 3, "<>": 3, "<": 3, "<=": 3, ">": 3, ">=": 3, "LIKE": 3, "+": 4, "-": 4, "*": 5, "/": 5, "%": 5}

func (p *parser) parseExpr(min int) (Expr, error) {
	left, err := p.parsePrefix()
	if err != nil {
		return nil, err
	}
	for {
		if p.keywordPeek("IS") {
			if 3 < min {
				break
			}
			pos := p.next().pos
			not := p.keyword("NOT")
			if !p.keyword("NULL") {
				return nil, p.err(p.peek(), "expected NULL after IS")
			}
			left = &IsNullExpr{Pos: pos, Not: not, X: left}
			continue
		}
		not := false
		if p.keywordPeek("NOT") && (p.keywordAt(1, "IN") || p.keywordAt(1, "BETWEEN") || p.keywordAt(1, "LIKE")) {
			if 3 < min {
				break
			}
			p.next()
			not = true
		}
		if p.keywordPeek("IN") {
			if 3 < min {
				break
			}
			pos := p.next().pos
			if !p.match(tokLParen) {
				return nil, p.err(p.peek(), "expected '(' after IN")
			}
			x := &InExpr{Pos: pos, Not: not, X: left}
			if p.keywordPeek("SELECT") || p.keywordPeek("WITH") {
				x.Query, err = p.parseSelect()
			} else {
				x.List, err = p.exprList()
			}
			if err != nil {
				return nil, err
			}
			if !p.match(tokRParen) {
				return nil, p.err(p.peek(), "expected ')' after IN")
			}
			left = x
			continue
		}
		if p.keywordPeek("BETWEEN") {
			if 3 < min {
				break
			}
			pos := p.next().pos
			lo, er := p.parseExpr(4)
			if er != nil {
				return nil, er
			}
			if !p.keyword("AND") {
				return nil, p.err(p.peek(), "expected AND in BETWEEN")
			}
			hi, er := p.parseExpr(4)
			if er != nil {
				return nil, er
			}
			left = &BetweenExpr{Pos: pos, Not: not, X: left, Low: lo, High: hi}
			continue
		}
		op, ok := p.binaryOp()
		if !ok {
			if not {
				return nil, p.err(p.peek(), "invalid NOT expression")
			}
			break
		}
		prec := precedence[op]
		if prec < min {
			break
		}
		pos := p.next().pos
		right, er := p.parseExpr(prec + 1)
		if er != nil {
			return nil, er
		}
		bin := Expr(&BinaryExpr{Pos: pos, Op: op, Left: left, Right: right})
		if not {
			bin = &UnaryExpr{Pos: pos, Op: "NOT", X: bin}
		}
		left = bin
	}
	return left, nil
}

func (p *parser) parsePrefix() (Expr, error) {
	t := p.next()
	switch t.kind {
	case tokNumber:
		if strings.Contains(t.lit, ".") {
			v, _ := strconv.ParseFloat(t.lit, 64)
			return &Literal{Pos: t.pos, Value: v}, nil
		}
		v, _ := strconv.ParseInt(t.lit, 10, 64)
		return &Literal{Pos: t.pos, Value: v}, nil
	case tokString:
		return &Literal{Pos: t.pos, Value: t.lit}, nil
	case tokParameter:
		return &Parameter{Pos: t.pos, Name: t.lit}, nil
	case tokStar:
		return &Star{Pos: t.pos}, nil
	case tokPlus, tokMinus:
		x, err := p.parseExpr(6)
		return &UnaryExpr{Pos: t.pos, Op: t.lit, X: x}, err
	case tokLParen:
		e, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if !p.match(tokRParen) {
			return nil, p.err(p.peek(), "expected ')'")
		}
		return e, nil
	case tokIdent:
		upper := strings.ToUpper(t.lit)
		switch upper {
		case "NULL":
			return &Literal{Pos: t.pos, Value: nil}, nil
		case "TRUE":
			return &Literal{Pos: t.pos, Value: true}, nil
		case "FALSE":
			return &Literal{Pos: t.pos, Value: false}, nil
		case "NOT":
			x, err := p.parseExpr(6)
			return &UnaryExpr{Pos: t.pos, Op: "NOT", X: x}, err
		case "EXISTS":
			if !p.match(tokLParen) {
				return nil, p.err(p.peek(), "expected '(' after EXISTS")
			}
			q, err := p.parseSelect()
			if err != nil {
				return nil, err
			}
			if !p.match(tokRParen) {
				return nil, p.err(p.peek(), "expected ')' after EXISTS")
			}
			return &ExistsExpr{Pos: t.pos, Query: q}, nil
		case "CASE":
			return p.parseCase(t.pos)
		case "CAST":
			return p.parseCast(t.pos)
		}
		if p.match(tokLParen) {
			call := &CallExpr{Pos: t.pos, Name: t.lit}
			call.Distinct = p.keyword("DISTINCT")
			if !p.match(tokRParen) {
				for {
					e, err := p.parseExpr(0)
					if err != nil {
						return nil, err
					}
					call.Args = append(call.Args, e)
					if p.match(tokRParen) {
						break
					}
					if !p.match(tokComma) {
						return nil, p.err(p.peek(), "expected ',' or ')' in function call")
					}
				}
			}
			return call, nil
		}
		id := &Identifier{Pos: t.pos, Name: t.lit}
		if p.match(tokDot) {
			if p.match(tokStar) {
				return &Star{Pos: t.pos, Qualifier: t.lit}, nil
			}
			id.Qualifier = t.lit
			name, err := p.ident()
			if err != nil {
				return nil, err
			}
			id.Name = name
		}
		return id, nil
	}
	return nil, p.err(t, "expected expression")
}

func (p *parser) parseCase(pos Position) (Expr, error) {
	c := &CaseExpr{Pos: pos}
	if !p.keywordPeek("WHEN") {
		x, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		c.Base = x
	}
	for p.keyword("WHEN") {
		w, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if !p.keyword("THEN") {
			return nil, p.err(p.peek(), "expected THEN")
		}
		v, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		c.Whens = append(c.Whens, CaseWhen{When: w, Then: v})
	}
	if p.keyword("ELSE") {
		x, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		c.Else = x
	}
	if !p.keyword("END") {
		return nil, p.err(p.peek(), "expected END")
	}
	return c, nil
}
func (p *parser) parseCast(pos Position) (Expr, error) {
	if !p.match(tokLParen) {
		return nil, p.err(p.peek(), "expected '(' after CAST")
	}
	x, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if !p.keyword("AS") {
		return nil, p.err(p.peek(), "expected AS in CAST")
	}
	typ, err := p.ident()
	if err != nil {
		return nil, err
	}
	if !p.match(tokRParen) {
		return nil, p.err(p.peek(), "expected ')' after CAST")
	}
	return &CastExpr{Pos: pos, X: x, Type: typ}, nil
}

func (p *parser) binaryOp() (string, bool) {
	t := p.peek()
	switch t.kind {
	case tokEq, tokNE, tokLT, tokLE, tokGT, tokGE, tokPlus, tokMinus, tokStar, tokSlash, tokPercent:
		return t.lit, true
	}
	if t.kind == tokIdent {
		u := strings.ToUpper(t.lit)
		if _, ok := precedence[u]; ok {
			return u, true
		}
	}
	return "", false
}
func (p *parser) intLiteral() (int, error) {
	t := p.next()
	if t.kind != tokNumber || strings.Contains(t.lit, ".") {
		return 0, p.err(t, "expected non-negative integer")
	}
	v, err := strconv.Atoi(t.lit)
	if err != nil || v < 0 {
		return 0, p.err(t, "invalid non-negative integer")
	}
	return v, nil
}
func (p *parser) ident() (string, error) {
	t := p.next()
	if t.kind != tokIdent {
		return "", p.err(t, "expected identifier")
	}
	return t.lit, nil
}
func (p *parser) keyword(w string) bool {
	if p.keywordPeek(w) {
		p.i++
		return true
	}
	return false
}
func (p *parser) keywordPeek(w string) bool { return p.keywordAt(0, w) }
func (p *parser) keywordAt(n int, w string) bool {
	j := p.i + n
	return j < len(p.tokens) && p.tokens[j].kind == tokIdent && strings.EqualFold(p.tokens[j].lit, w)
}
func (p *parser) match(k tokenKind) bool {
	if p.peek().kind == k {
		p.i++
		return true
	}
	return false
}
func (p *parser) peek() token {
	if p.i >= len(p.tokens) {
		return token{kind: tokEOF}
	}
	return p.tokens[p.i]
}
func (p *parser) next() token {
	t := p.peek()
	if p.i < len(p.tokens) {
		p.i++
	}
	return t
}
func (p *parser) err(t token, msg string) error { return sqlErr("sql_parse_error", t.pos, msg) }
func isClauseKeyword(s string) bool {
	switch strings.ToUpper(s) {
	case "FROM", "WHERE", "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "CROSS", "ON", "GROUP", "HAVING", "ORDER", "LIMIT", "OFFSET", "UNION", "ASC", "DESC", "NULLS", "FIRST", "LAST":
		return true
	}
	return false
}
func (p *parser) debug() string { return fmt.Sprintf("token=%+v", p.peek()) }
