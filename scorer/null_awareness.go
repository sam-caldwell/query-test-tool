package scorer

import (
	"fmt"

	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/sam-caldwell/query-test-tool/parser"
)

// Penalty accessors for NULL-awareness rules.
func PenaltyNullCoalesceInPredicate() int { return Weight("null-coalesce-in-predicate") }
func PenaltyNullCheckChain() int          { return Weight("null-check-chain") }

// scoreNullPatterns detects NULL-related anti-patterns in the query AST.
// COALESCE findings go to the efficiency dimension, null-check-chain to cognitive.
func scoreNullPatterns(tree *pg_query.ParseResult, eff *DimensionScore, cog *DimensionScore) {
	for _, stmt := range tree.Stmts {
		scoreNullNode(stmt.Stmt, eff, cog)
	}
}

func scoreNullNode(node *pg_query.Node, eff *DimensionScore, cog *DimensionScore) {
	if node == nil {
		return
	}

	parser.Walk(node, 0, func(n *pg_query.Node, depth int) bool {
		if sel, ok := n.Node.(*pg_query.Node_SelectStmt); ok {
			checkCoalesceInPredicate(sel.SelectStmt, eff)
			checkNullCheckChain(sel.SelectStmt, cog)
		}
		return true
	})
}

// checkCoalesceInPredicate detects COALESCE (or FuncCall "coalesce") used inside
// WHERE or JOIN ON clauses. Wrapping a column in COALESCE prevents index usage
// on the underlying column.
func checkCoalesceInPredicate(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil {
		return
	}

	// Check WHERE clause for CoalesceExpr nodes.
	if sel.WhereClause != nil {
		findCoalesceInExpr(sel.WhereClause, ds)
	}

	// Check JOIN ON clauses.
	for _, from := range sel.FromClause {
		findCoalesceInJoins(from, ds)
	}
}

// findCoalesceInExpr walks an expression tree looking for CoalesceExpr nodes
// that wrap column references, indicating a non-indexable predicate.
func findCoalesceInExpr(node *pg_query.Node, ds *DimensionScore) {
	if node == nil {
		return
	}

	parser.Walk(node, 0, func(n *pg_query.Node, depth int) bool {
		switch v := n.Node.(type) {
		case *pg_query.Node_CoalesceExpr:
			// Check if any argument is a column reference.
			if containsColumnRef(v.CoalesceExpr.Args) {
				ds.Findings = append(ds.Findings, Finding{
					Rule:        "null-coalesce-in-predicate",
					Description: "COALESCE() on column in predicate prevents index usage",
					Penalty:     PenaltyNullCoalesceInPredicate(),
					Category:    "efficiency",
				})
				ds.Score += PenaltyNullCoalesceInPredicate()
			}
			return false // don't recurse into children (already checked args)
		case *pg_query.Node_FuncCall:
			// pg_query sometimes parses COALESCE as a FuncCall with funcname "coalesce".
			name := funcName(v.FuncCall)
			if name == "coalesce" || name == "pg_catalog.coalesce" {
				if containsColumnRef(v.FuncCall.Args) {
					ds.Findings = append(ds.Findings, Finding{
						Rule:        "null-coalesce-in-predicate",
						Description: "COALESCE() on column in predicate prevents index usage",
						Penalty:     PenaltyNullCoalesceInPredicate(),
						Category:    "efficiency",
					})
					ds.Score += PenaltyNullCoalesceInPredicate()
				}
			}
			return false
		}
		return true
	})
}

// findCoalesceInJoins checks JOIN ON clauses for COALESCE usage.
func findCoalesceInJoins(node *pg_query.Node, ds *DimensionScore) {
	if node == nil {
		return
	}
	if j, ok := node.Node.(*pg_query.Node_JoinExpr); ok {
		if j.JoinExpr.Quals != nil {
			findCoalesceInExpr(j.JoinExpr.Quals, ds)
		}
		findCoalesceInJoins(j.JoinExpr.Larg, ds)
		findCoalesceInJoins(j.JoinExpr.Rarg, ds)
	}
}

// checkNullCheckChain detects 3+ IS NULL / IS NOT NULL checks in a single
// query, which indicates complex NULL handling that adds cognitive load
// and plan branches.
func checkNullCheckChain(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil {
		return
	}

	count := 0

	// Count NullTest nodes anywhere in the SELECT (WHERE, HAVING, JOIN, etc.)
	countNullTests(sel.WhereClause, &count)
	countNullTests(sel.HavingClause, &count)
	for _, from := range sel.FromClause {
		countNullTestsInJoins(from, &count)
	}

	if count >= 3 {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "null-check-chain",
			Description: fmt.Sprintf("%d IS NULL/IS NOT NULL checks add cognitive complexity and plan branches", count),
			Penalty:     PenaltyNullCheckChain(),
			Category:    "cognitive_complexity",
		})
		ds.Score += PenaltyNullCheckChain()
	}
}

// countNullTests walks an expression tree and counts NullTest nodes.
func countNullTests(node *pg_query.Node, count *int) {
	if node == nil {
		return
	}
	parser.Walk(node, 0, func(n *pg_query.Node, depth int) bool {
		if _, ok := n.Node.(*pg_query.Node_NullTest); ok {
			*count++
		}
		return true
	})
}

// countNullTestsInJoins counts NullTest nodes in JOIN ON clauses.
func countNullTestsInJoins(node *pg_query.Node, count *int) {
	if node == nil {
		return
	}
	if j, ok := node.Node.(*pg_query.Node_JoinExpr); ok {
		countNullTests(j.JoinExpr.Quals, count)
		countNullTestsInJoins(j.JoinExpr.Larg, count)
		countNullTestsInJoins(j.JoinExpr.Rarg, count)
	}
}
