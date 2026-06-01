package scorer

import (
	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/sam-caldwell/query-test-tool/src/parser"
)

// Cognitive complexity penalty accessors — values loaded from embedded weights.json.
func PenaltySubqueryNesting() int { return Weight("subquery-nesting") }
func PenaltyJoin() int            { return Weight("join") }
func PenaltyOuterJoin() int       { return Weight("outer-join") }
func PenaltyBooleanNesting() int  { return Weight("boolean-nesting") }
func PenaltyCTE() int             { return Weight("cte") }
func PenaltyCaseExpr() int        { return Weight("case-expression") }
func PenaltySetOperation() int    { return Weight("set-operation") }

// CognitiveScorer adapts a cyclomatic-style metric to SQL readability.
type CognitiveScorer struct{}

func (s *CognitiveScorer) Score(tree *pg_query.ParseResult) DimensionScore {
	ds := DimensionScore{Name: "cognitive_complexity"}

	for _, stmt := range tree.Stmts {
		s.scoreNode(stmt.Stmt, 0, &ds)
	}
	return ds
}

func (s *CognitiveScorer) scoreNode(node *pg_query.Node, subqueryDepth int, ds *DimensionScore) {
	if node == nil {
		return
	}

	switch v := node.Node.(type) {
	case *pg_query.Node_SelectStmt:
		s.scoreSelect(v.SelectStmt, subqueryDepth, ds)
	default:
		// Walk other statement types for embedded selects.
		parser.Walk(node, 0, func(n *pg_query.Node, depth int) bool {
			switch inner := n.Node.(type) {
			case *pg_query.Node_SelectStmt:
				s.scoreSelect(inner.SelectStmt, subqueryDepth, ds)
				return false // we handle children ourselves
			}
			return true
		})
	}
}

func (s *CognitiveScorer) scoreSelect(sel *pg_query.SelectStmt, subqueryDepth int, ds *DimensionScore) {
	if sel == nil {
		return
	}

	// Set operations (UNION, INTERSECT, EXCEPT).
	if sel.Op != pg_query.SetOperation_SETOP_NONE {
		penalty := PenaltySetOperation()
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "set-operation",
			Description: "Set operation (UNION/INTERSECT/EXCEPT) adds cognitive complexity",
			Penalty:     penalty,
			Category:    "cognitive_complexity",
		})
		ds.Score += penalty
		if sel.Larg != nil {
			s.scoreSelect(sel.Larg, subqueryDepth, ds)
		}
		if sel.Rarg != nil {
			s.scoreSelect(sel.Rarg, subqueryDepth, ds)
		}
		return
	}

	// Count joins in FROM clause.
	for _, from := range sel.FromClause {
		s.countJoins(from, ds)
	}

	// Count boolean nesting in WHERE.
	if sel.WhereClause != nil {
		s.scoreBooleanNesting(sel.WhereClause, 0, ds)
	}

	// Count boolean nesting in HAVING.
	if sel.HavingClause != nil {
		s.scoreBooleanNesting(sel.HavingClause, 0, ds)
	}

	// CTEs.
	if sel.WithClause != nil {
		for _, cte := range sel.WithClause.Ctes {
			ds.Findings = append(ds.Findings, Finding{
				Rule:        "cte",
				Description: "Common Table Expression adds a named scope to understand",
				Penalty:     PenaltyCTE(),
				Category:    "cognitive_complexity",
			})
			ds.Score += PenaltyCTE()
			// Score the CTE body.
			if cteNode, ok := cte.Node.(*pg_query.Node_CommonTableExpr); ok {
				s.scoreNode(cteNode.CommonTableExpr.Ctequery, subqueryDepth+1, ds)
			}
		}
	}

	// CASE expressions in target list.
	for _, target := range sel.TargetList {
		if rt, ok := target.Node.(*pg_query.Node_ResTarget); ok && rt.ResTarget != nil {
			s.scoreCaseExprs(rt.ResTarget.Val, ds)
		}
	}

	// Subqueries in WHERE clause.
	s.scoreSubqueries(sel.WhereClause, subqueryDepth, ds)

	// Subqueries in target list (scalar subqueries).
	for _, target := range sel.TargetList {
		if rt, ok := target.Node.(*pg_query.Node_ResTarget); ok && rt.ResTarget != nil {
			s.scoreSubqueries(rt.ResTarget.Val, subqueryDepth, ds)
		}
	}

	// Subqueries in FROM clause (derived tables).
	for _, from := range sel.FromClause {
		s.scoreFromSubqueries(from, subqueryDepth, ds)
	}

	// Subqueries in HAVING clause.
	s.scoreSubqueries(sel.HavingClause, subqueryDepth, ds)
}

func (s *CognitiveScorer) countJoins(node *pg_query.Node, ds *DimensionScore) {
	if node == nil {
		return
	}
	if j, ok := node.Node.(*pg_query.Node_JoinExpr); ok {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "join",
			Description: "Each JOIN adds a relationship to reason about",
			Penalty:     PenaltyJoin(),
			Category:    "cognitive_complexity",
		})
		ds.Score += PenaltyJoin()

		// Outer joins (LEFT, RIGHT, FULL) get an additional penalty because
		// they produce NULL-padded rows that inflate intermediate result sets
		// and force downstream operations into three-valued logic.
		jt := j.JoinExpr.Jointype
		if jt == pg_query.JoinType_JOIN_LEFT || jt == pg_query.JoinType_JOIN_RIGHT || jt == pg_query.JoinType_JOIN_FULL {
			penalty := PenaltyOuterJoin()
			ds.Findings = append(ds.Findings, Finding{
				Rule:        "outer-join",
				Description: "LEFT/RIGHT/FULL JOIN produces NULL-padded rows increasing downstream cost",
				Penalty:     penalty,
				Category:    "cognitive_complexity",
			})
			ds.Score += penalty
		}

		// Recurse into nested joins.
		s.countJoins(j.JoinExpr.Larg, ds)
		s.countJoins(j.JoinExpr.Rarg, ds)
	}
}

func (s *CognitiveScorer) scoreBooleanNesting(node *pg_query.Node, depth int, ds *DimensionScore) {
	if node == nil {
		return
	}
	if b, ok := node.Node.(*pg_query.Node_BoolExpr); ok {
		if depth > 0 {
			penalty := PenaltyBooleanNesting() * depth
			ds.Findings = append(ds.Findings, Finding{
				Rule:        "boolean-nesting",
				Description: "Nested boolean expression increases cognitive load",
				Penalty:     penalty,
				Category:    "cognitive_complexity",
			})
			ds.Score += penalty
		}
		for _, arg := range b.BoolExpr.Args {
			s.scoreBooleanNesting(arg, depth+1, ds)
		}
	}
}

func (s *CognitiveScorer) scoreCaseExprs(node *pg_query.Node, ds *DimensionScore) {
	if node == nil {
		return
	}
	parser.Walk(node, 0, func(n *pg_query.Node, depth int) bool {
		if _, ok := n.Node.(*pg_query.Node_CaseExpr); ok {
			ds.Findings = append(ds.Findings, Finding{
				Rule:        "case-expression",
				Description: "CASE expression adds conditional branching logic",
				Penalty:     PenaltyCaseExpr(),
				Category:    "cognitive_complexity",
			})
			ds.Score += PenaltyCaseExpr()
		}
		return true
	})
}

func (s *CognitiveScorer) scoreSubqueries(node *pg_query.Node, parentDepth int, ds *DimensionScore) {
	if node == nil {
		return
	}
	parser.Walk(node, 0, func(n *pg_query.Node, depth int) bool {
		if sl, ok := n.Node.(*pg_query.Node_SubLink); ok {
			newDepth := parentDepth + 1
			penalty := PenaltySubqueryNesting() * newDepth
			ds.Findings = append(ds.Findings, Finding{
				Rule:        "subquery-nesting",
				Description: "Subquery at nesting depth increases complexity",
				Penalty:     penalty,
				Category:    "cognitive_complexity",
			})
			ds.Score += penalty
			// Score the subquery's contents.
			if sl.SubLink.Subselect != nil {
				s.scoreNode(sl.SubLink.Subselect, newDepth, ds)
			}
			return false // we handle children ourselves
		}
		return true
	})
}

func (s *CognitiveScorer) scoreFromSubqueries(node *pg_query.Node, parentDepth int, ds *DimensionScore) {
	if node == nil {
		return
	}
	switch v := node.Node.(type) {
	case *pg_query.Node_RangeSubselect:
		newDepth := parentDepth + 1
		penalty := PenaltySubqueryNesting() * newDepth
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "subquery-nesting",
			Description: "Derived table (subquery in FROM) at nesting depth increases complexity",
			Penalty:     penalty,
			Category:    "cognitive_complexity",
		})
		ds.Score += penalty
		s.scoreNode(v.RangeSubselect.Subquery, newDepth, ds)
	case *pg_query.Node_JoinExpr:
		s.scoreFromSubqueries(v.JoinExpr.Larg, parentDepth, ds)
		s.scoreFromSubqueries(v.JoinExpr.Rarg, parentDepth, ds)
	}
}
