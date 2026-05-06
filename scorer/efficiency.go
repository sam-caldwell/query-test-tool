package scorer

import (
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/sam-caldwell/query-test-tool/parser"
)

// Efficiency penalty accessors — values loaded from embedded weights.json.
func PenaltySelectStar() int       { return Weight("select-star") }
func PenaltyMissingPredicate() int { return Weight("missing-predicate") }
func PenaltyCorrelatedSubq() int   { return Weight("correlated-subquery") }
func PenaltyNonSargable() int      { return Weight("non-sargable") }
func PenaltyDistinctDedup() int    { return Weight("distinct-dedup") }

// EfficiencyScorer detects anti-patterns that prevent optimal query execution.
type EfficiencyScorer struct{}

func (s *EfficiencyScorer) Score(tree *pg_query.ParseResult) DimensionScore {
	ds := DimensionScore{Name: "efficiency"}

	for _, stmt := range tree.Stmts {
		s.scoreNode(stmt.Stmt, 0, &ds)
	}
	return ds
}

func (s *EfficiencyScorer) scoreNode(node *pg_query.Node, depth int, ds *DimensionScore) {
	if node == nil {
		return
	}

	parser.Walk(node, depth, func(n *pg_query.Node, d int) bool {
		switch v := n.Node.(type) {
		case *pg_query.Node_SelectStmt:
			s.checkSelectStar(v.SelectStmt, ds)
			s.checkMissingPredicates(v.SelectStmt, ds)
			s.checkDistinctDedup(v.SelectStmt, ds)
			s.checkNonSargable(v.SelectStmt, ds)
		case *pg_query.Node_SubLink:
			s.checkCorrelatedSubquery(v.SubLink, ds)
		}
		return true
	})
}

// checkSelectStar flags SELECT * usage.
func (s *EfficiencyScorer) checkSelectStar(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil {
		return
	}
	for _, target := range sel.TargetList {
		rt, ok := target.Node.(*pg_query.Node_ResTarget)
		if !ok || rt.ResTarget == nil || rt.ResTarget.Val == nil {
			continue
		}
		if _, isStar := rt.ResTarget.Val.Node.(*pg_query.Node_ColumnRef); isStar {
			cr := rt.ResTarget.Val.GetColumnRef()
			if cr != nil {
				for _, f := range cr.Fields {
					if _, ok := f.Node.(*pg_query.Node_AStar); ok {
						ds.Findings = append(ds.Findings, Finding{
							Rule:        "select-star",
							Description: "SELECT * prevents index-only scans and fetches unnecessary columns",
							Penalty:     PenaltySelectStar(),
							Category:    "efficiency",
						})
						ds.Score += PenaltySelectStar()
						return // only report once per SELECT
					}
				}
			}
		}
	}
}

// checkMissingPredicates flags JOINs that have no ON/USING clause (implicit cross joins).
func (s *EfficiencyScorer) checkMissingPredicates(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil {
		return
	}

	// Check for multiple tables in FROM without WHERE (implicit cross join pattern).
	rangeVarCount := 0
	for _, from := range sel.FromClause {
		if _, ok := from.Node.(*pg_query.Node_RangeVar); ok {
			rangeVarCount++
		}
	}
	if rangeVarCount >= 2 && sel.WhereClause == nil {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "missing-predicate",
			Description: "Multiple tables in FROM without WHERE clause — likely missing join predicate",
			Penalty:     PenaltyMissingPredicate(),
			Category:    "efficiency",
		})
		ds.Score += PenaltyMissingPredicate()
	}
}

// checkCorrelatedSubquery flags subqueries that reference outer tables.
func (s *EfficiencyScorer) checkCorrelatedSubquery(sl *pg_query.SubLink, ds *DimensionScore) {
	if sl == nil {
		return
	}
	// Flag sublinks: EXISTS with a correlated WHERE, or IN/ANY/ALL with a Testexpr.
	isCorrelated := false
	switch sl.SubLinkType {
	case pg_query.SubLinkType_EXISTS_SUBLINK:
		// EXISTS subqueries are correlated if their inner WHERE references outer columns.
		// We flag all EXISTS as potentially correlated since that's their typical usage.
		isCorrelated = true
	case pg_query.SubLinkType_ANY_SUBLINK, pg_query.SubLinkType_ALL_SUBLINK:
		// IN / ANY / ALL with a test expression
		isCorrelated = sl.Testexpr != nil
	case pg_query.SubLinkType_EXPR_SUBLINK:
		isCorrelated = sl.Testexpr != nil
	}
	if isCorrelated {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "correlated-subquery",
			Description: "Correlated subquery may execute once per outer row",
			Penalty:     PenaltyCorrelatedSubq(),
			Category:    "efficiency",
		})
		ds.Score += PenaltyCorrelatedSubq()
	}
}

// nonSargableFuncs are functions that, when applied to a column in WHERE, prevent index usage.
// Keys are bare function names; schema prefixes (e.g. pg_catalog.) are stripped before lookup.
var nonSargableFuncs = map[string]bool{
	"upper": true, "lower": true, "trim": true, "ltrim": true, "rtrim": true, "btrim": true,
	"substr": true, "substring": true, "left": true, "right": true,
	"replace": true, "translate": true, "concat": true,
	"to_char": true, "to_date": true, "to_timestamp": true, "to_number": true,
	"cast": true, "coalesce": true,
	"date_trunc": true, "extract": true, "date_part": true,
	"abs": true, "ceil": true, "floor": true, "round": true, "trunc": true,
	"length": true, "char_length": true, "octet_length": true,
}

// isNonSargable checks if a function name (possibly schema-qualified) is non-sargable.
func isNonSargable(name string) bool {
	if nonSargableFuncs[name] {
		return true
	}
	// Strip schema prefix (e.g. pg_catalog.btrim -> btrim)
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return nonSargableFuncs[name[idx+1:]]
	}
	return false
}

// checkNonSargable flags function calls wrapping columns in WHERE predicates.
func (s *EfficiencyScorer) checkNonSargable(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil || sel.WhereClause == nil {
		return
	}
	s.findNonSargable(sel.WhereClause, ds)
}

func (s *EfficiencyScorer) findNonSargable(node *pg_query.Node, ds *DimensionScore) {
	if node == nil {
		return
	}

	switch v := node.Node.(type) {
	case *pg_query.Node_BoolExpr:
		for _, arg := range v.BoolExpr.Args {
			s.findNonSargable(arg, ds)
		}
	case *pg_query.Node_AExpr:
		// Check if either side of a comparison wraps a column in a function.
		s.checkExprForNonSargable(v.AExpr.Lexpr, ds)
		s.checkExprForNonSargable(v.AExpr.Rexpr, ds)
	}
}

func (s *EfficiencyScorer) checkExprForNonSargable(node *pg_query.Node, ds *DimensionScore) {
	if node == nil {
		return
	}
	if fc, ok := node.Node.(*pg_query.Node_FuncCall); ok {
		name := funcName(fc.FuncCall)
		if isNonSargable(name) && containsColumnRef(fc.FuncCall.Args) {
			displayName := name
			if idx := strings.LastIndex(displayName, "."); idx >= 0 {
				displayName = displayName[idx+1:]
			}
			ds.Findings = append(ds.Findings, Finding{
				Rule:        "non-sargable",
				Description: "Function " + strings.ToUpper(displayName) + "() on column prevents index usage",
				Penalty:     PenaltyNonSargable(),
				Category:    "efficiency",
			})
			ds.Score += PenaltyNonSargable()
		}
	}
}

// checkDistinctDedup flags DISTINCT on queries with JOINs, which often indicates
// a missing or incorrect join predicate causing row duplication.
func (s *EfficiencyScorer) checkDistinctDedup(sel *pg_query.SelectStmt, ds *DimensionScore) {
	if sel == nil || len(sel.DistinctClause) == 0 {
		return
	}
	hasJoin := false
	for _, from := range sel.FromClause {
		if _, ok := from.Node.(*pg_query.Node_JoinExpr); ok {
			hasJoin = true
			break
		}
	}
	if hasJoin {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "distinct-dedup",
			Description: "DISTINCT with JOIN suggests join produces duplicates — fix the join or use GROUP BY",
			Penalty:     PenaltyDistinctDedup(),
			Category:    "efficiency",
		})
		ds.Score += PenaltyDistinctDedup()
	}
}

// containsColumnRef checks if any node in the slice is a ColumnRef.
func containsColumnRef(nodes []*pg_query.Node) bool {
	for _, n := range nodes {
		if n == nil {
			continue
		}
		if _, ok := n.Node.(*pg_query.Node_ColumnRef); ok {
			return true
		}
		// Check nested — e.g., function args might contain column refs
		if fc, ok := n.Node.(*pg_query.Node_FuncCall); ok {
			if containsColumnRef(fc.FuncCall.Args) {
				return true
			}
		}
		if tc, ok := n.Node.(*pg_query.Node_TypeCast); ok {
			if tc.TypeCast.Arg != nil {
				if _, ok := tc.TypeCast.Arg.Node.(*pg_query.Node_ColumnRef); ok {
					return true
				}
			}
		}
	}
	return false
}
