// Package scorer provides SQL query scoring across efficiency, memory/compute, and cognitive complexity.
package scorer

import (
	"fmt"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/sam-caldwell/query-test-tool/src/dialect"
	"github.com/sam-caldwell/query-test-tool/src/parser"
)

// Type aliases from dialect package — keeps backward compatibility for all
// existing code that references scorer.Finding, scorer.Report, etc.
type Finding = dialect.Finding
type DimensionScore = dialect.DimensionScore
type Report = dialect.Report

// Scorer scores a parsed SQL statement (PostgreSQL-specific).
type Scorer interface {
	Score(tree *pg_query.ParseResult) DimensionScore
}

// ScoreQuery parses and scores a SQL query using the active dialect.
func ScoreQuery(sql string) (*Report, error) {
	return ScoreQueryWithDialect(sql, ActiveDialect())
}

// DialectScorer is a function that scores SQL for a specific dialect.
// Register dialect-specific scorers via RegisterDialectScorer to avoid import cycles.
type DialectScorer func(sql string) (*Report, error)

var dialectScorers = map[dialect.Dialect]DialectScorer{}

// RegisterDialectScorer registers a scoring function for a dialect.
// This is called from cmd/query-test-tool or other entry points that can import
// dialect-specific packages without creating import cycles.
func RegisterDialectScorer(d dialect.Dialect, fn DialectScorer) {
	dialectScorers[d] = fn
}

// ScoreQueryWithDialect parses and scores a SQL query using the specified dialect.
func ScoreQueryWithDialect(sql string, d dialect.Dialect) (*Report, error) {
	SetDialect(d)

	if fn, ok := dialectScorers[d]; ok {
		return fn(sql)
	}

	// Default: PostgreSQL scorer
	if d == dialect.PostgreSQL {
		return scorePostgreSQL(sql)
	}

	return nil, fmt.Errorf("no scorer registered for dialect: %s", d)
}

// scorePostgreSQL scores a SQL query using the PostgreSQL parser and scorers.
func scorePostgreSQL(sql string) (*Report, error) {
	tree, err := parser.Parse(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SQL: %w", err)
	}

	eff := (&EfficiencyScorer{}).Score(tree)
	mem := (&MemoryComputeScorer{}).Score(tree)
	cog := (&CognitiveScorer{}).Score(tree)

	// Apply superlinear join cost escalation.
	applyJoinEscalation(&cog)

	// NULL awareness: COALESCE in predicates → efficiency, NULL check chains → cognitive.
	scoreNullPatterns(tree, &eff, &cog)

	// Function cost: expensive and volatile functions → efficiency.
	scoreFunctionCost(tree, &eff)

	// DML patterns: missing WHERE, large offset, recursive CTE, etc.
	scoreDMLPatterns(tree, &eff, &mem, &cog)

	// DDL patterns: CREATE, ALTER, DROP, CASCADE.
	scoreDDLPatterns(tree, &cog)

	return &Report{
		SQL:              sql,
		Dialect:          string(ActiveDialect()),
		TotalScore:       eff.Score + mem.Score + cog.Score,
		Efficiency:       eff,
		MemoryCompute:    mem,
		CognitiveComplex: cog,
	}, nil
}

// applyJoinEscalation adds a superlinear (quadratic) penalty when multiple joins
// are present. The extra penalty captures optimizer complexity and fan-out compounding
// that makes each additional join more expensive than the last.
func applyJoinEscalation(ds *DimensionScore) {
	joinCount := 0
	for _, f := range ds.Findings {
		if f.Rule == "join" {
			joinCount++
		}
	}
	if joinCount <= 1 {
		return
	}
	// Quadratic portion beyond linear: count² - count
	quadratic := joinCount*joinCount - joinCount
	penalty := Weight("join-count-squared") * quadratic
	if penalty > 0 {
		ds.Findings = append(ds.Findings, Finding{
			Rule:        "join-escalation",
			Description: fmt.Sprintf("Superlinear join cost: %d joins compound optimizer complexity", joinCount),
			Penalty:     penalty,
			Category:    "cognitive_complexity",
		})
		ds.Score += penalty
	}
}

// funcName extracts the function name from a FuncCall node.
func funcName(fc *pg_query.FuncCall) string {
	if fc == nil {
		return ""
	}
	var parts []string
	for _, n := range fc.Funcname {
		if s, ok := n.Node.(*pg_query.Node_String_); ok {
			parts = append(parts, s.String_.Sval)
		}
	}
	return strings.ToLower(strings.Join(parts, "."))
}

// isAggregate returns true if the function name is a common SQL aggregate.
var aggregateFuncs = map[string]bool{
	"count": true, "sum": true, "avg": true, "min": true, "max": true,
	"array_agg": true, "string_agg": true, "json_agg": true,
	"jsonb_agg": true, "bool_and": true, "bool_or": true,
	"every": true, "xmlagg": true, "json_object_agg": true,
	"jsonb_object_agg": true,
}

func isAggregate(name string) bool {
	return aggregateFuncs[name]
}
