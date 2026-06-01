// Package mysql provides SQL scoring for MySQL dialect using the vitess sqlparser.
package mysql

import (
	"fmt"
	"strings"

	"github.com/xwb1989/sqlparser"

	"github.com/sam-caldwell/query-test-tool/src/dialect"
)

// ScoreQuery parses and scores a SQL query using the MySQL parser.
func ScoreQuery(sql string) (*dialect.Report, error) {
	stmt, err := sqlparser.Parse(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to parse MySQL SQL: %w", err)
	}

	eff := scoreEfficiency(stmt)
	mem := scoreMemoryCompute(stmt)
	cog := scoreCognitive(stmt)

	return &dialect.Report{
		SQL:              sql,
		Dialect:          "mysql",
		TotalScore:       eff.Score + mem.Score + cog.Score,
		Efficiency:       eff,
		MemoryCompute:    mem,
		CognitiveComplex: cog,
	}, nil
}

func scoreEfficiency(stmt sqlparser.Statement) dialect.DimensionScore {
	ds := dialect.DimensionScore{Name: "efficiency"}

	sel, ok := stmt.(*sqlparser.Select)
	if !ok {
		// For non-SELECT (INSERT, UPDATE, DELETE, DDL), do basic checks
		scoreNonSelect(stmt, &ds)
		return ds
	}

	// SELECT * detection
	for _, expr := range sel.SelectExprs {
		if _, ok := expr.(*sqlparser.StarExpr); ok {
			p := dialect.Weight("select-star")
			ds.Findings = append(ds.Findings, dialect.Finding{
				Rule:        "select-star",
				Description: "SELECT * returns all columns; specify only needed columns",
				Penalty:     p,
				Category:    "efficiency",
			})
			ds.Score += p
		}
	}

	// Non-sargable: function calls in WHERE predicates
	if sel.Where != nil {
		walkExpr(sel.Where.Expr, func(e sqlparser.Expr) {
			if fc, ok := e.(*sqlparser.FuncExpr); ok {
				// Check if function wraps a column (non-sargable)
				for _, arg := range fc.Exprs {
					if _, ok := arg.(*sqlparser.AliasedExpr); ok {
						p := dialect.Weight("non-sargable")
						ds.Findings = append(ds.Findings, dialect.Finding{
							Rule:        "non-sargable",
							Description: fmt.Sprintf("Function %s() in WHERE prevents index usage", fc.Name.String()),
							Penalty:     p,
							Category:    "efficiency",
						})
						ds.Score += p
						return
					}
				}
			}
		})
	}

	// Missing predicate (no WHERE on SELECT)
	if sel.Where == nil && len(sel.From) > 0 {
		p := dialect.Weight("missing-predicate")
		ds.Findings = append(ds.Findings, dialect.Finding{
			Rule:        "missing-predicate",
			Description: "Query has no WHERE clause — full table scan",
			Penalty:     p,
			Category:    "efficiency",
		})
		ds.Score += p
	}

	// LIKE with leading wildcard
	if sel.Where != nil {
		walkExpr(sel.Where.Expr, func(e sqlparser.Expr) {
			if comp, ok := e.(*sqlparser.ComparisonExpr); ok && comp.Operator == "like" {
				if val, ok := comp.Right.(*sqlparser.SQLVal); ok {
					if strings.HasPrefix(string(val.Val), "%") {
						p := dialect.Weight("like-leading-wildcard")
						ds.Findings = append(ds.Findings, dialect.Finding{
							Rule:        "like-leading-wildcard",
							Description: "LIKE with leading wildcard prevents index usage",
							Penalty:     p,
							Category:    "efficiency",
						})
						ds.Score += p
					}
				}
			}
		})
	}

	// Large OFFSET
	if sel.Limit != nil && sel.Limit.Offset != nil {
		if val, ok := sel.Limit.Offset.(*sqlparser.SQLVal); ok {
			if val.Type == sqlparser.IntVal {
				var n int
				fmt.Sscanf(string(val.Val), "%d", &n)
				if n > 1000 {
					p := dialect.Weight("large-offset")
					ds.Findings = append(ds.Findings, dialect.Finding{
						Rule:        "large-offset",
						Description: fmt.Sprintf("OFFSET %d forces scanning and discarding rows", n),
						Penalty:     p,
						Category:    "efficiency",
					})
					ds.Score += p
				}
			}
		}
	}

	return ds
}

func scoreMemoryCompute(stmt sqlparser.Statement) dialect.DimensionScore {
	ds := dialect.DimensionScore{Name: "memory_compute"}

	sel, ok := stmt.(*sqlparser.Select)
	if !ok {
		return ds
	}

	// DISTINCT
	if sel.Distinct != "" {
		p := dialect.Weight("distinct-dedup")
		ds.Findings = append(ds.Findings, dialect.Finding{
			Rule:        "distinct-dedup",
			Description: "DISTINCT requires sorting/hashing all result rows",
			Penalty:     p,
			Category:    "memory_compute",
		})
		ds.Score += p
	}

	// ORDER BY without LIMIT (unbounded sort)
	if len(sel.OrderBy) > 0 && sel.Limit == nil {
		p := dialect.Weight("unbounded-sort")
		ds.Findings = append(ds.Findings, dialect.Finding{
			Rule:        "unbounded-sort",
			Description: "ORDER BY without LIMIT sorts entire result set in memory",
			Penalty:     p,
			Category:    "memory_compute",
		})
		ds.Score += p
	}

	// GROUP BY
	if len(sel.GroupBy) > 0 {
		p := dialect.Weight("group-by-fanout")
		ds.Findings = append(ds.Findings, dialect.Finding{
			Rule:        "group-by-fanout",
			Description: "GROUP BY requires hash/sort aggregation",
			Penalty:     p,
			Category:    "memory_compute",
		})
		ds.Score += p
	}

	return ds
}

func scoreCognitive(stmt sqlparser.Statement) dialect.DimensionScore {
	ds := dialect.DimensionScore{Name: "cognitive_complexity"}

	sel, ok := stmt.(*sqlparser.Select)
	if !ok {
		scoreDDL(stmt, &ds)
		return ds
	}

	// JOIN count
	joinCount := 0
	for _, from := range sel.From {
		countJoins(from, &joinCount)
	}
	for i := 0; i < joinCount; i++ {
		p := dialect.Weight("join")
		ds.Findings = append(ds.Findings, dialect.Finding{
			Rule:        "join",
			Description: "JOIN adds optimizer complexity",
			Penalty:     p,
			Category:    "cognitive_complexity",
		})
		ds.Score += p
	}

	// Join escalation (quadratic)
	if joinCount > 1 {
		quadratic := joinCount*joinCount - joinCount
		p := dialect.Weight("join-count-squared") * quadratic
		if p > 0 {
			ds.Findings = append(ds.Findings, dialect.Finding{
				Rule:        "join-escalation",
				Description: fmt.Sprintf("Superlinear join cost: %d joins compound optimizer complexity", joinCount),
				Penalty:     p,
				Category:    "cognitive_complexity",
			})
			ds.Score += p
		}
	}

	// Outer joins
	for _, from := range sel.From {
		walkTableExpr(from, func(je *sqlparser.JoinTableExpr) {
			if je.Join == sqlparser.LeftJoinStr || je.Join == sqlparser.RightJoinStr {
				p := dialect.Weight("outer-join")
				ds.Findings = append(ds.Findings, dialect.Finding{
					Rule:        "outer-join",
					Description: fmt.Sprintf("%s JOIN may produce NULL-extended rows", je.Join),
					Penalty:     p,
					Category:    "cognitive_complexity",
				})
				ds.Score += p
			}
		})
	}

	// Subqueries
	subqueryCount := 0
	walkExpr(stmtToExpr(sel), func(e sqlparser.Expr) {
		if _, ok := e.(*sqlparser.Subquery); ok {
			subqueryCount++
		}
	})
	for i := 0; i < subqueryCount; i++ {
		p := dialect.Weight("subquery-nesting")
		ds.Findings = append(ds.Findings, dialect.Finding{
			Rule:        "subquery-nesting",
			Description: "Subquery adds execution complexity",
			Penalty:     p,
			Category:    "cognitive_complexity",
		})
		ds.Score += p
	}

	// Boolean nesting (AND/OR combinations in WHERE)
	if sel.Where != nil {
		depth := boolDepth(sel.Where.Expr)
		if depth > 1 {
			p := dialect.Weight("boolean-nesting")
			ds.Findings = append(ds.Findings, dialect.Finding{
				Rule:        "boolean-nesting",
				Description: fmt.Sprintf("Nested boolean logic (depth %d) adds cognitive complexity", depth),
				Penalty:     p,
				Category:    "cognitive_complexity",
			})
			ds.Score += p
		}
	}

	// CASE expressions
	caseCount := 0
	walkSelectExprs(sel.SelectExprs, func(e sqlparser.Expr) {
		if _, ok := e.(*sqlparser.CaseExpr); ok {
			caseCount++
		}
	})
	for i := 0; i < caseCount; i++ {
		p := dialect.Weight("case-expression")
		ds.Findings = append(ds.Findings, dialect.Finding{
			Rule:        "case-expression",
			Description: "CASE expression adds branching complexity",
			Penalty:     p,
			Category:    "cognitive_complexity",
		})
		ds.Score += p
	}

	// UNION / UNION ALL
	if _, ok := stmt.(*sqlparser.Union); ok {
		p := dialect.Weight("set-operation")
		ds.Findings = append(ds.Findings, dialect.Finding{
			Rule:        "set-operation",
			Description: "UNION combines multiple result sets",
			Penalty:     p,
			Category:    "cognitive_complexity",
		})
		ds.Score += p
	}

	return ds
}

func scoreNonSelect(stmt sqlparser.Statement, ds *dialect.DimensionScore) {
	switch s := stmt.(type) {
	case *sqlparser.Update:
		if s.Where == nil {
			p := dialect.Weight("missing-where-clause")
			ds.Findings = append(ds.Findings, dialect.Finding{
				Rule:        "missing-where-clause",
				Description: "UPDATE without WHERE affects all rows",
				Penalty:     p,
				Category:    "efficiency",
			})
			ds.Score += p
		}
	case *sqlparser.Delete:
		if s.Where == nil {
			p := dialect.Weight("missing-where-clause")
			ds.Findings = append(ds.Findings, dialect.Finding{
				Rule:        "missing-where-clause",
				Description: "DELETE without WHERE affects all rows",
				Penalty:     p,
				Category:    "efficiency",
			})
			ds.Score += p
		}
	}
}

func scoreDDL(stmt sqlparser.Statement, ds *dialect.DimensionScore) {
	switch stmt.(type) {
	case *sqlparser.DDL:
		p := dialect.Weight("ddl-statement")
		ds.Findings = append(ds.Findings, dialect.Finding{
			Rule:        "ddl-statement",
			Description: "DDL statement modifies schema",
			Penalty:     p,
			Category:    "cognitive_complexity",
		})
		ds.Score += p
	}
}

// --- AST walking helpers ---

func countJoins(te sqlparser.TableExpr, count *int) {
	switch t := te.(type) {
	case *sqlparser.JoinTableExpr:
		*count++
		countJoins(t.LeftExpr, count)
		countJoins(t.RightExpr, count)
	case *sqlparser.ParenTableExpr:
		for _, expr := range t.Exprs {
			countJoins(expr, count)
		}
	}
}

func walkTableExpr(te sqlparser.TableExpr, fn func(*sqlparser.JoinTableExpr)) {
	switch t := te.(type) {
	case *sqlparser.JoinTableExpr:
		fn(t)
		walkTableExpr(t.LeftExpr, fn)
		walkTableExpr(t.RightExpr, fn)
	case *sqlparser.ParenTableExpr:
		for _, expr := range t.Exprs {
			walkTableExpr(expr, fn)
		}
	}
}

func walkExpr(e sqlparser.Expr, fn func(sqlparser.Expr)) {
	if e == nil {
		return
	}
	fn(e)
	switch expr := e.(type) {
	case *sqlparser.AndExpr:
		walkExpr(expr.Left, fn)
		walkExpr(expr.Right, fn)
	case *sqlparser.OrExpr:
		walkExpr(expr.Left, fn)
		walkExpr(expr.Right, fn)
	case *sqlparser.NotExpr:
		walkExpr(expr.Expr, fn)
	case *sqlparser.ComparisonExpr:
		walkExpr(expr.Left, fn)
		walkExpr(expr.Right, fn)
	case *sqlparser.ParenExpr:
		walkExpr(expr.Expr, fn)
	case *sqlparser.FuncExpr:
		for _, arg := range expr.Exprs {
			if ae, ok := arg.(*sqlparser.AliasedExpr); ok {
				walkExpr(ae.Expr, fn)
			}
		}
	case *sqlparser.CaseExpr:
		if expr.Expr != nil {
			walkExpr(expr.Expr, fn)
		}
		for _, when := range expr.Whens {
			walkExpr(when.Cond, fn)
			walkExpr(when.Val, fn)
		}
		if expr.Else != nil {
			walkExpr(expr.Else, fn)
		}
	case *sqlparser.Subquery:
		// Don't recurse into subqueries — counted separately
	}
}

func walkSelectExprs(exprs sqlparser.SelectExprs, fn func(sqlparser.Expr)) {
	for _, se := range exprs {
		if ae, ok := se.(*sqlparser.AliasedExpr); ok {
			walkExpr(ae.Expr, fn)
		}
	}
}

func stmtToExpr(sel *sqlparser.Select) sqlparser.Expr {
	// Walk WHERE and HAVING for subquery detection
	if sel.Where != nil {
		return sel.Where.Expr
	}
	return nil
}

func boolDepth(e sqlparser.Expr) int {
	switch expr := e.(type) {
	case *sqlparser.AndExpr:
		l := boolDepth(expr.Left)
		r := boolDepth(expr.Right)
		if l > r {
			return l + 1
		}
		return r + 1
	case *sqlparser.OrExpr:
		l := boolDepth(expr.Left)
		r := boolDepth(expr.Right)
		if l > r {
			return l + 1
		}
		return r + 1
	case *sqlparser.ParenExpr:
		return boolDepth(expr.Expr)
	default:
		return 0
	}
}
