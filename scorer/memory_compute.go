package scorer

import (
	pg_query "github.com/pganalyze/pg_query_go/v5"

	"github.com/sqlscore/parser"
)

// Memory/Compute penalty accessors — values loaded from embedded weights.json.
func PenaltyUnboundedSort() int    { return Weight("unbounded-sort") }
func PenaltyGroupByFanout() int    { return Weight("group-by-fanout") }
func PenaltyWindowFunction() int   { return Weight("window-function") }
func PenaltyCartesianProduct() int { return Weight("cartesian-product") }
func PenaltyNoPartition() int      { return Weight("window-no-partition-extra") }

// MemoryComputeScorer scores operations that require materializing intermediate results.
type MemoryComputeScorer struct{}

func (s *MemoryComputeScorer) Score(tree *pg_query.ParseResult) DimensionScore {
	ds := DimensionScore{Name: "memory_compute"}

	for _, stmt := range tree.Stmts {
		s.scoreNode(stmt.Stmt, &ds)
	}
	return ds
}

func (s *MemoryComputeScorer) scoreNode(node *pg_query.Node, ds *DimensionScore) {
	if node == nil {
		return
	}

	parser.Walk(node, 0, func(n *pg_query.Node, depth int) bool {
		if sel, ok := n.Node.(*pg_query.Node_SelectStmt); ok {
			s.checkUnboundedSort(sel.SelectStmt, ds)
			s.checkGroupByFanout(sel.SelectStmt, ds)
			s.checkCartesianProduct(sel.SelectStmt, ds)
			s.checkWindowFunctions(sel.SelectStmt, ds)
		}
		return true
	})
}

// checkUnboundedSort flags ORDER BY without LIMIT.
func (s *MemoryComputeScorer) checkUnboundedSort(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil {
		return
	}
	if len(sel.SortClause) > 0 && sel.LimitCount == nil && sel.LimitOffset == nil {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "unbounded-sort",
			Description: "ORDER BY without LIMIT requires materializing and sorting the entire result set",
			Penalty:     PenaltyUnboundedSort(),
			Category:    "memory_compute",
		})
		ds.Score += PenaltyUnboundedSort()
	}
}

// checkGroupByFanout flags GROUP BY with aggregation that could cause intermediate materialization.
func (s *MemoryComputeScorer) checkGroupByFanout(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil || len(sel.GroupClause) == 0 {
		return
	}
	// Check if the select list has aggregate functions.
	hasAgg := false
	for _, target := range sel.TargetList {
		if rt, ok := target.Node.(*pg_query.Node_ResTarget); ok && rt.ResTarget != nil {
			if hasAggregateInExpr(rt.ResTarget.Val) {
				hasAgg = true
				break
			}
		}
	}
	if hasAgg {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "group-by-fanout",
			Description: "GROUP BY with aggregation requires materializing groups in memory",
			Penalty:     PenaltyGroupByFanout(),
			Category:    "memory_compute",
		})
		ds.Score += PenaltyGroupByFanout()
	}
}

// checkCartesianProduct flags explicit CROSS JOINs or implicit cross joins.
func (s *MemoryComputeScorer) checkCartesianProduct(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil {
		return
	}

	// Check for CROSS JOIN in join expressions.
	for _, from := range sel.FromClause {
		if s.hasCrossJoin(from) {
			ds.Findings = append(ds.Findings, Finding{
				Rule:        "cartesian-product",
				Description: "CROSS JOIN produces a Cartesian product — O(n*m) rows",
				Penalty:     PenaltyCartesianProduct(),
				Category:    "memory_compute",
			})
			ds.Score += PenaltyCartesianProduct()
			return
		}
	}

	// Check for implicit cross join: multiple tables in FROM with no WHERE.
	rangeVarCount := 0
	for _, from := range sel.FromClause {
		if _, ok := from.Node.(*pg_query.Node_RangeVar); ok {
			rangeVarCount++
		}
	}
	if rangeVarCount >= 2 && sel.WhereClause == nil {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "cartesian-product",
			Description: "Implicit cross join (multiple tables without WHERE) produces Cartesian product",
			Penalty:     PenaltyCartesianProduct(),
			Category:    "memory_compute",
		})
		ds.Score += PenaltyCartesianProduct()
	}
}

func (s *MemoryComputeScorer) hasCrossJoin(node *pg_query.Node) bool {
	if node == nil {
		return false
	}
	if j, ok := node.Node.(*pg_query.Node_JoinExpr); ok {
		je := j.JoinExpr
		// A JOIN with no quals and no USING clause and type INNER is effectively a cross join.
		// Also JoinType_JOIN_TYPE_UNDEFINED with no quals is a cross join.
		if je.Quals == nil && len(je.UsingClause) == 0 {
			return true
		}
		return s.hasCrossJoin(je.Larg) || s.hasCrossJoin(je.Rarg)
	}
	return false
}

// checkWindowFunctions flags window function usage.
func (s *MemoryComputeScorer) checkWindowFunctions(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil {
		return
	}
	for _, target := range sel.TargetList {
		rt, ok := target.Node.(*pg_query.Node_ResTarget)
		if !ok || rt.ResTarget == nil {
			continue
		}
		s.findWindowFunctions(rt.ResTarget.Val, ds)
	}
}

func (s *MemoryComputeScorer) findWindowFunctions(node *pg_query.Node, ds *DimensionScore) {
	if node == nil {
		return
	}
	if fc, ok := node.Node.(*pg_query.Node_FuncCall); ok {
		if fc.FuncCall.Over != nil {
			penalty := PenaltyWindowFunction()
			desc := "Window function requires maintaining state across the partition"
			if len(fc.FuncCall.Over.PartitionClause) == 0 {
				penalty += PenaltyNoPartition()
				desc = "Window function without PARTITION BY operates over entire result set"
			}
			ds.Findings = append(ds.Findings, Finding{
				Rule:        "window-function",
				Description: desc,
				Penalty:     penalty,
				Category:    "memory_compute",
			})
			ds.Score += penalty
		}
	}
}

// hasAggregateInExpr checks if a node contains an aggregate function call.
func hasAggregateInExpr(node *pg_query.Node) bool {
	if node == nil {
		return false
	}
	found := false
	parser.Walk(node, 0, func(n *pg_query.Node, depth int) bool {
		if found {
			return false
		}
		if fc, ok := n.Node.(*pg_query.Node_FuncCall); ok {
			name := funcName(fc.FuncCall)
			if isAggregate(name) || fc.FuncCall.AggStar {
				found = true
				return false
			}
		}
		return true
	})
	return found
}
