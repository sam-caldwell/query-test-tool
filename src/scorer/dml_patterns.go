package scorer

import (
	"fmt"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/sam-caldwell/query-test-tool/src/parser"
)

// DML pattern penalty accessors — values loaded from embedded weights.json,
// falling back to zero until these rules are added to the weights file.
func PenaltyMissingWhereClause() int      { return Weight("missing-where-clause") }
func PenaltyLargeOffset() int             { return Weight("large-offset") }
func PenaltyRecursiveCTE() int            { return Weight("recursive-cte") }
func PenaltyLargeInList() int             { return Weight("large-in-list") }
func PenaltyLikeLeadingWildcard() int     { return Weight("like-leading-wildcard") }
func PenaltyImplicitCastInPredicate() int { return Weight("implicit-cast-in-predicate") }
func PenaltyLateralJoin() int             { return Weight("lateral-join") }
func PenaltyReturningClause() int         { return Weight("returning-clause") }
func PenaltyGroupingSets() int            { return Weight("grouping-sets") }
func PenaltyForUpdateLock() int           { return Weight("for-update-lock") }
func PenaltyUnionDistinct() int           { return Weight("union-distinct") }

// largeInListThreshold is the number of literal values in an IN clause
// above which we suggest using ANY(ARRAY[...]) instead.
const largeInListThreshold = 20

// largeOffsetThreshold is the minimum literal OFFSET value that triggers
// the large-offset warning.
const largeOffsetThreshold = 100

// scoreDMLPatterns detects DML anti-patterns in the query AST.
// Findings are added to the appropriate dimension score.
func scoreDMLPatterns(tree *pg_query.ParseResult, eff *DimensionScore, mem *DimensionScore, cog *DimensionScore) {
	for _, stmt := range tree.Stmts {
		scoreDMLNode(stmt.Stmt, eff, mem, cog)
	}
}

func scoreDMLNode(node *pg_query.Node, eff *DimensionScore, mem *DimensionScore, cog *DimensionScore) {
	if node == nil {
		return
	}

	parser.Walk(node, 0, func(n *pg_query.Node, depth int) bool {
		switch v := n.Node.(type) {
		case *pg_query.Node_UpdateStmt:
			checkMissingWhereUpdate(v.UpdateStmt, eff)
			checkReturningUpdate(v.UpdateStmt, cog)
		case *pg_query.Node_DeleteStmt:
			checkMissingWhereDelete(v.DeleteStmt, eff)
			checkReturningDelete(v.DeleteStmt, cog)
		case *pg_query.Node_InsertStmt:
			checkReturningInsert(v.InsertStmt, cog)
		case *pg_query.Node_SelectStmt:
			checkLargeOffset(v.SelectStmt, eff)
			checkRecursiveCTE(v.SelectStmt, mem)
			checkGroupingSets(v.SelectStmt, mem)
			checkForUpdateLock(v.SelectStmt, eff)
			checkUnionDistinct(v.SelectStmt, eff)
			checkLateralJoin(v.SelectStmt, mem)
			checkLargeInList(v.SelectStmt, eff)
			checkLikeLeadingWildcard(v.SelectStmt, eff)
			checkImplicitCastInPredicate(v.SelectStmt, eff)
		}
		return true
	})
}

// checkMissingWhereUpdate flags UPDATE statements without a WHERE clause.
func checkMissingWhereUpdate(stmt *pg_query.UpdateStmt, ds *DimensionScore) {
	if stmt == nil {
		return
	}
	if stmt.WhereClause == nil {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "missing-where-clause",
			Description: "UPDATE without WHERE clause affects all rows — potentially dangerous",
			Penalty:     PenaltyMissingWhereClause(),
			Category:    "efficiency",
		})
		ds.Score += PenaltyMissingWhereClause()
	}
}

// checkMissingWhereDelete flags DELETE statements without a WHERE clause.
func checkMissingWhereDelete(stmt *pg_query.DeleteStmt, ds *DimensionScore) {
	if stmt == nil {
		return
	}
	if stmt.WhereClause == nil {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "missing-where-clause",
			Description: "DELETE without WHERE clause removes all rows — potentially dangerous",
			Penalty:     PenaltyMissingWhereClause(),
			Category:    "efficiency",
		})
		ds.Score += PenaltyMissingWhereClause()
	}
}

// checkReturningInsert flags INSERT with RETURNING clause.
func checkReturningInsert(stmt *pg_query.InsertStmt, ds *DimensionScore) {
	if stmt == nil {
		return
	}
	if len(stmt.ReturningList) > 0 {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "returning-clause",
			Description: "RETURNING clause adds result-set overhead to INSERT",
			Penalty:     PenaltyReturningClause(),
			Category:    "cognitive_complexity",
		})
		ds.Score += PenaltyReturningClause()
	}
}

// checkReturningUpdate flags UPDATE with RETURNING clause.
func checkReturningUpdate(stmt *pg_query.UpdateStmt, ds *DimensionScore) {
	if stmt == nil {
		return
	}
	if len(stmt.ReturningList) > 0 {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "returning-clause",
			Description: "RETURNING clause adds result-set overhead to UPDATE",
			Penalty:     PenaltyReturningClause(),
			Category:    "cognitive_complexity",
		})
		ds.Score += PenaltyReturningClause()
	}
}

// checkReturningDelete flags DELETE with RETURNING clause.
func checkReturningDelete(stmt *pg_query.DeleteStmt, ds *DimensionScore) {
	if stmt == nil {
		return
	}
	if len(stmt.ReturningList) > 0 {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "returning-clause",
			Description: "RETURNING clause adds result-set overhead to DELETE",
			Penalty:     PenaltyReturningClause(),
			Category:    "cognitive_complexity",
		})
		ds.Score += PenaltyReturningClause()
	}
}

// checkLargeOffset flags LIMIT...OFFSET where offset is a literal > largeOffsetThreshold.
func checkLargeOffset(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil || sel.LimitOffset == nil {
		return
	}
	// Check if the offset is a constant integer > threshold.
	if ac, ok := sel.LimitOffset.Node.(*pg_query.Node_AConst); ok {
		if iv, ok := ac.AConst.Val.(*pg_query.A_Const_Ival); ok {
			if iv.Ival.Ival > int32(largeOffsetThreshold) {
				ds.Findings = append(ds.Findings, Finding{
					Rule:        "large-offset",
					Description: fmt.Sprintf("OFFSET %d requires scanning and discarding rows — use keyset pagination instead", iv.Ival.Ival),
					Penalty:     PenaltyLargeOffset(),
					Category:    "efficiency",
				})
				ds.Score += PenaltyLargeOffset()
			}
		}
	}
}

// checkRecursiveCTE flags WITH RECURSIVE CTEs.
func checkRecursiveCTE(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil || sel.WithClause == nil {
		return
	}
	if sel.WithClause.Recursive {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "recursive-cte",
			Description: "WITH RECURSIVE iterates until fixpoint — significantly more expensive than regular CTE",
			Penalty:     PenaltyRecursiveCTE(),
			Category:    "memory_compute",
		})
		ds.Score += PenaltyRecursiveCTE()
	}
}

// checkGroupingSets flags GROUPING SETS, CUBE, and ROLLUP in GROUP BY.
func checkGroupingSets(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil || len(sel.GroupClause) == 0 {
		return
	}
	for _, g := range sel.GroupClause {
		if gs, ok := g.Node.(*pg_query.Node_GroupingSet); ok {
			kind := gs.GroupingSet.Kind
			if kind == pg_query.GroupingSetKind_GROUPING_SET_ROLLUP ||
				kind == pg_query.GroupingSetKind_GROUPING_SET_CUBE ||
				kind == pg_query.GroupingSetKind_GROUPING_SET_SETS {
				var label string
				switch kind {
				case pg_query.GroupingSetKind_GROUPING_SET_ROLLUP:
					label = "ROLLUP"
				case pg_query.GroupingSetKind_GROUPING_SET_CUBE:
					label = "CUBE"
				default:
					label = "GROUPING SETS"
				}
				ds.Findings = append(ds.Findings, Finding{
					Rule:        "grouping-sets",
					Description: fmt.Sprintf("%s requires multiple aggregation passes over the data", label),
					Penalty:     PenaltyGroupingSets(),
					Category:    "memory_compute",
				})
				ds.Score += PenaltyGroupingSets()
				return // only flag once per SELECT
			}
		}
	}
}

// checkForUpdateLock flags SELECT ... FOR UPDATE / FOR SHARE.
func checkForUpdateLock(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil || len(sel.LockingClause) == 0 {
		return
	}
	ds.Findings = append(ds.Findings, Finding{
		Rule:        "for-update-lock",
		Description: "FOR UPDATE/FOR SHARE acquires row-level locks increasing contention",
		Penalty:     PenaltyForUpdateLock(),
		Category:    "efficiency",
	})
	ds.Score += PenaltyForUpdateLock()
}

// checkUnionDistinct flags UNION (without ALL) which implies DISTINCT/sort.
func checkUnionDistinct(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil {
		return
	}
	if sel.Op == pg_query.SetOperation_SETOP_UNION && !sel.All {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "union-distinct",
			Description: "UNION (without ALL) implies an implicit DISTINCT requiring sort/dedup — use UNION ALL if duplicates are acceptable",
			Penalty:     PenaltyUnionDistinct(),
			Category:    "efficiency",
		})
		ds.Score += PenaltyUnionDistinct()
	}
}

// checkLateralJoin flags LATERAL subqueries in FROM clause.
func checkLateralJoin(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil {
		return
	}
	for _, from := range sel.FromClause {
		findLateralInFrom(from, ds)
	}
}

func findLateralInFrom(node *pg_query.Node, ds *DimensionScore) {
	if node == nil {
		return
	}
	switch v := node.Node.(type) {
	case *pg_query.Node_RangeSubselect:
		if v.RangeSubselect.Lateral {
			ds.Findings = append(ds.Findings, Finding{
				Rule:        "lateral-join",
				Description: "LATERAL subquery executes once per outer row — consider alternatives",
				Penalty:     PenaltyLateralJoin(),
				Category:    "memory_compute",
			})
			ds.Score += PenaltyLateralJoin()
		}
	case *pg_query.Node_RangeFunction:
		if v.RangeFunction.Lateral {
			ds.Findings = append(ds.Findings, Finding{
				Rule:        "lateral-join",
				Description: "LATERAL function executes once per outer row — consider alternatives",
				Penalty:     PenaltyLateralJoin(),
				Category:    "memory_compute",
			})
			ds.Score += PenaltyLateralJoin()
		}
	case *pg_query.Node_JoinExpr:
		findLateralInFrom(v.JoinExpr.Larg, ds)
		findLateralInFrom(v.JoinExpr.Rarg, ds)
	}
}

// checkLargeInList flags IN clauses with more than largeInListThreshold literal values.
func checkLargeInList(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil || sel.WhereClause == nil {
		return
	}
	findLargeInList(sel.WhereClause, ds)
}

func findLargeInList(node *pg_query.Node, ds *DimensionScore) {
	if node == nil {
		return
	}

	parser.Walk(node, 0, func(n *pg_query.Node, depth int) bool {
		ae, ok := n.Node.(*pg_query.Node_AExpr)
		if !ok {
			return true
		}
		if ae.AExpr.Kind != pg_query.A_Expr_Kind_AEXPR_IN {
			return true
		}
		// The right side of IN is a List of values.
		if ae.AExpr.Rexpr == nil {
			return true
		}
		if lst, ok := ae.AExpr.Rexpr.Node.(*pg_query.Node_List); ok {
			if len(lst.List.Items) > largeInListThreshold {
				ds.Findings = append(ds.Findings, Finding{
					Rule:        "large-in-list",
					Description: fmt.Sprintf("IN list with %d values — consider using ANY(ARRAY[...]) instead", len(lst.List.Items)),
					Penalty:     PenaltyLargeInList(),
					Category:    "efficiency",
				})
				ds.Score += PenaltyLargeInList()
			}
		}
		return true
	})
}

// checkLikeLeadingWildcard flags LIKE/ILIKE patterns starting with '%'.
func checkLikeLeadingWildcard(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil || sel.WhereClause == nil {
		return
	}
	findLikeLeadingWildcard(sel.WhereClause, ds)
}

func findLikeLeadingWildcard(node *pg_query.Node, ds *DimensionScore) {
	if node == nil {
		return
	}

	parser.Walk(node, 0, func(n *pg_query.Node, depth int) bool {
		ae, ok := n.Node.(*pg_query.Node_AExpr)
		if !ok {
			return true
		}
		if ae.AExpr.Kind != pg_query.A_Expr_Kind_AEXPR_LIKE &&
			ae.AExpr.Kind != pg_query.A_Expr_Kind_AEXPR_ILIKE {
			return true
		}
		// Check if the right side is a string constant starting with '%'.
		if ae.AExpr.Rexpr == nil {
			return true
		}
		if ac, ok := ae.AExpr.Rexpr.Node.(*pg_query.Node_AConst); ok {
			if sv, ok := ac.AConst.Val.(*pg_query.A_Const_Sval); ok {
				if strings.HasPrefix(sv.Sval.Sval, "%") {
					ds.Findings = append(ds.Findings, Finding{
						Rule:        "like-leading-wildcard",
						Description: "LIKE/ILIKE with leading wildcard prevents index usage — forces sequential scan",
						Penalty:     PenaltyLikeLeadingWildcard(),
						Category:    "efficiency",
					})
					ds.Score += PenaltyLikeLeadingWildcard()
				}
			}
		}
		return true
	})
}

// checkImplicitCastInPredicate flags type casts on columns in WHERE (e.g., col::text = 'val').
func checkImplicitCastInPredicate(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil || sel.WhereClause == nil {
		return
	}
	findImplicitCast(sel.WhereClause, ds)
}

func findImplicitCast(node *pg_query.Node, ds *DimensionScore) {
	if node == nil {
		return
	}

	parser.Walk(node, 0, func(n *pg_query.Node, depth int) bool {
		switch v := n.Node.(type) {
		case *pg_query.Node_AExpr:
			checkExprForCast(v.AExpr.Lexpr, ds)
			checkExprForCast(v.AExpr.Rexpr, ds)
		}
		return true
	})
}

// checkExprForCast checks if a node is a TypeCast wrapping a ColumnRef.
func checkExprForCast(node *pg_query.Node, ds *DimensionScore) {
	if node == nil {
		return
	}
	if tc, ok := node.Node.(*pg_query.Node_TypeCast); ok {
		if tc.TypeCast.Arg != nil {
			if _, ok := tc.TypeCast.Arg.Node.(*pg_query.Node_ColumnRef); ok {
				ds.Findings = append(ds.Findings, Finding{
					Rule:        "implicit-cast-in-predicate",
					Description: "Type cast on column in WHERE prevents index usage — non-sargable",
					Penalty:     PenaltyImplicitCastInPredicate(),
					Category:    "efficiency",
				})
				ds.Score += PenaltyImplicitCastInPredicate()
			}
		}
	}
}
