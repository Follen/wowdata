package sqlquery

import "fmt"

const Dialect = "wowdata-sql-v1"

type Position struct {
	Offset int `json:"offset"`
	Line   int `json:"line"`
	Column int `json:"column"`
}

type Error struct {
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Position Position `json:"position"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s at %d:%d: %s", e.Code, e.Position.Line, e.Position.Column, e.Message)
}

type Statement struct {
	Explain bool
	Analyze bool
	Query   *Select
}

type Select struct {
	Pos       Position
	Recursive bool
	CTEs      []CTE
	Distinct  bool
	Items     []SelectItem
	From      TableRef
	Joins     []Join
	Where     Expr
	GroupBy   []Expr
	Having    Expr
	OrderBy   []OrderTerm
	Limit     *int
	Offset    int
	UnionAll  *Select
}

type CTE struct {
	Name  string
	Query *Select
}

type TableRef struct {
	Pos      Position
	Catalog  string
	Name     string
	Alias    string
	Subquery *Select
}

func (t TableRef) EffectiveAlias() string {
	if t.Alias != "" {
		return t.Alias
	}
	return t.Name
}

type JoinType string

const (
	JoinInner JoinType = "inner"
	JoinLeft  JoinType = "left"
	JoinCross JoinType = "cross"
)

type Join struct {
	Pos   Position
	Type  JoinType
	Table TableRef
	On    Expr
}

type SelectItem struct {
	Expr  Expr
	Alias string
}

type OrderTerm struct {
	Expr       Expr
	Desc       bool
	NullsFirst *bool
}

type Expr interface {
	Position() Position
	exprNode()
}

type Identifier struct {
	Pos       Position
	Qualifier string
	Name      string
}

func (e *Identifier) Position() Position { return e.Pos }
func (e *Identifier) exprNode()          {}

type Star struct {
	Pos       Position
	Qualifier string
}

func (e *Star) Position() Position { return e.Pos }
func (e *Star) exprNode()          {}

type Literal struct {
	Pos   Position
	Value any
}

func (e *Literal) Position() Position { return e.Pos }
func (e *Literal) exprNode()          {}

type Parameter struct {
	Pos  Position
	Name string
}

func (e *Parameter) Position() Position { return e.Pos }
func (e *Parameter) exprNode()          {}

type UnaryExpr struct {
	Pos Position
	Op  string
	X   Expr
}

func (e *UnaryExpr) Position() Position { return e.Pos }
func (e *UnaryExpr) exprNode()          {}

type BinaryExpr struct {
	Pos         Position
	Op          string
	Left, Right Expr
}

func (e *BinaryExpr) Position() Position { return e.Pos }
func (e *BinaryExpr) exprNode()          {}

type InExpr struct {
	Pos   Position
	Not   bool
	X     Expr
	List  []Expr
	Query *Select
}

func (e *InExpr) Position() Position { return e.Pos }
func (e *InExpr) exprNode()          {}

type BetweenExpr struct {
	Pos          Position
	Not          bool
	X, Low, High Expr
}

func (e *BetweenExpr) Position() Position { return e.Pos }
func (e *BetweenExpr) exprNode()          {}

type IsNullExpr struct {
	Pos Position
	Not bool
	X   Expr
}

func (e *IsNullExpr) Position() Position { return e.Pos }
func (e *IsNullExpr) exprNode()          {}

type CallExpr struct {
	Pos      Position
	Name     string
	Args     []Expr
	Distinct bool
}

func (e *CallExpr) Position() Position { return e.Pos }
func (e *CallExpr) exprNode()          {}

type CaseWhen struct {
	When Expr
	Then Expr
}

type CaseExpr struct {
	Pos   Position
	Base  Expr
	Whens []CaseWhen
	Else  Expr
}

func (e *CaseExpr) Position() Position { return e.Pos }
func (e *CaseExpr) exprNode()          {}

type CastExpr struct {
	Pos  Position
	X    Expr
	Type string
}

func (e *CastExpr) Position() Position { return e.Pos }
func (e *CastExpr) exprNode()          {}

type ExistsExpr struct {
	Pos   Position
	Not   bool
	Query *Select
}

func (e *ExistsExpr) Position() Position { return e.Pos }
func (e *ExistsExpr) exprNode()          {}
