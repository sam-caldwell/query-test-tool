// Package scorer provides SQL query scoring across efficiency, memory/compute, and cognitive complexity.
package scorer

import (
	"fmt"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v5"

	"github.com/sqlscore/parser"
)

// Finding represents a single issue detected in the query.
type Finding struct {
	Rule        string `json:"rule"`
	Description string `json:"description"`
	Penalty     int    `json:"penalty"`
	Category    string `json:"category"`
}

// DimensionScore holds the score for a single dimension.
type DimensionScore struct {
	Name     string    `json:"name"`
	Score    int       `json:"score"`
	Findings []Finding `json:"findings"`
}

// Report is the complete scoring result for a query.
type Report struct {
	SQL              string          `json:"sql"`
	TotalScore       int             `json:"total_score"`
	Efficiency       DimensionScore  `json:"efficiency"`
	MemoryCompute    DimensionScore  `json:"memory_compute"`
	CognitiveComplex DimensionScore  `json:"cognitive_complexity"`
}

// Scorer scores a parsed SQL statement.
type Scorer interface {
	Score(tree *pg_query.ParseResult) DimensionScore
}

// ScoreQuery parses and scores a SQL query string, returning a full Report.
func ScoreQuery(sql string) (*Report, error) {
	tree, err := parser.Parse(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SQL: %w", err)
	}

	eff := (&EfficiencyScorer{}).Score(tree)
	mem := (&MemoryComputeScorer{}).Score(tree)
	cog := (&CognitiveScorer{}).Score(tree)

	return &Report{
		SQL:              sql,
		TotalScore:       eff.Score + mem.Score + cog.Score,
		Efficiency:       eff,
		MemoryCompute:    mem,
		CognitiveComplex: cog,
	}, nil
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
