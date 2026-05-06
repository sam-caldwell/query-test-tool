package scorer

import (
	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/sam-caldwell/query-test-tool/parser"
)

// Function cost penalty accessors — values loaded from embedded weights.json,
// falling back to zero until these rules are added to the weights file.
func PenaltyExpensiveFunction() int { return Weight("expensive-function") }
func PenaltyVolatileFunction() int  { return Weight("volatile-function") }

// tier1Functions are expensive functions with high per-call cost.
var tier1Functions = map[string]bool{
	"regexp_match":            true,
	"regexp_replace":          true,
	"regexp_split_to_array":   true,
	"regexp_split_to_table":   true,
	"string_agg":              true,
	"array_agg":               true,
	"jsonb_agg":               true,
	"to_tsvector":             true,
	"to_tsquery":              true,
	"ts_rank":                 true,
}

// tier2Functions are common per-row functions that are cheap individually
// but indicate the query is doing too much transformation when many are used.
var tier2Functions = map[string]bool{
	"lower":        true,
	"upper":        true,
	"trim":         true,
	"concat":       true,
	"substring":    true,
	"replace":      true,
	"translate":    true,
	"to_char":      true,
	"to_date":      true,
	"to_timestamp": true,
	"to_number":    true,
}

// volatileFunctions prevent query plan caching and index usage.
var volatileFunctions = map[string]bool{
	"random":          true,
	"now":             true,
	"clock_timestamp": true,
	"timeofday":       true,
	"txid_current":    true,
	"nextval":         true,
	"currval":         true,
	"setval":          true,
	"pg_sleep":        true,
}

// tier2Threshold is the minimum number of distinct tier-2 function calls
// required in a single query before they are flagged.
const tier2Threshold = 3

// scoreFunctionCost walks the AST and detects expensive and volatile function calls.
func scoreFunctionCost(tree *pg_query.ParseResult, ds *DimensionScore) {
	var tier1Hits []string
	tier2Seen := make(map[string]bool)
	var volatileHits []string

	for _, stmt := range tree.Stmts {
		walkFunctionCost(stmt.Stmt, &tier1Hits, tier2Seen, &volatileHits)
	}

	// Emit findings for tier-1 expensive functions.
	for _, name := range tier1Hits {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "expensive-function",
			Description: "Expensive function " + name + "() has high per-call cost",
			Penalty:     PenaltyExpensiveFunction(),
			Category:    "efficiency",
		})
		ds.Score += PenaltyExpensiveFunction()
	}

	// Emit a single finding for tier-2 functions when the threshold is met.
	if len(tier2Seen) >= tier2Threshold {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "expensive-function",
			Description: "Query uses many per-row transformation functions, consider moving logic to the application",
			Penalty:     PenaltyExpensiveFunction(),
			Category:    "efficiency",
		})
		ds.Score += PenaltyExpensiveFunction()
	}

	// Emit findings for volatile functions.
	for _, name := range volatileHits {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "volatile-function",
			Description: "Volatile function " + name + "() prevents plan caching and index usage",
			Penalty:     PenaltyVolatileFunction(),
			Category:    "efficiency",
		})
		ds.Score += PenaltyVolatileFunction()
	}
}

// walkFunctionCost traverses the AST collecting function call classifications.
func walkFunctionCost(node *pg_query.Node, tier1Hits *[]string, tier2Seen map[string]bool, volatileHits *[]string) {
	if node == nil {
		return
	}

	parser.Walk(node, 0, func(n *pg_query.Node, depth int) bool {
		fc, ok := n.Node.(*pg_query.Node_FuncCall)
		if !ok {
			return true
		}

		name := funcName(fc.FuncCall)
		// Strip schema prefix for lookup.
		bare := name
		if idx := len(name) - 1; idx >= 0 {
			for i := len(name) - 1; i >= 0; i-- {
				if name[i] == '.' {
					bare = name[i+1:]
					break
				}
			}
		}

		switch {
		case tier1Functions[bare]:
			*tier1Hits = append(*tier1Hits, bare)
		case tier2Functions[bare]:
			tier2Seen[bare] = true
		case volatileFunctions[bare]:
			*volatileHits = append(*volatileHits, bare)
		}

		return true
	})
}
