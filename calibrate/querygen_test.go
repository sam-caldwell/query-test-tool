package calibrate

import (
	"math/rand"
	"strings"
	"testing"
)

func TestQueryGenerator_GenerateQueries(t *testing.T) {
	domain := Archetypes()[0] // ecommerce
	qg := NewQueryGenerator(42)

	queries := qg.GenerateQueries(domain, 1, 100)
	if len(queries) != 100 {
		t.Fatalf("expected 100 queries, got %d", len(queries))
	}

	queryTypes := make(map[string]int)
	for _, q := range queries {
		if q.SQL == "" {
			t.Error("query has empty SQL")
		}
		if q.FamilyID != 1 {
			t.Errorf("query has wrong family ID: %d", q.FamilyID)
		}
		if q.QueryType == "" {
			t.Error("query has empty type")
		}
		queryTypes[q.QueryType]++
	}

	// Should have variety
	if len(queryTypes) < 5 {
		t.Errorf("expected at least 5 different query types, got %d: %v", len(queryTypes), queryTypes)
	}
}

func TestQueryGenerator_AllDomains(t *testing.T) {
	for _, domain := range Archetypes() {
		qg := NewQueryGenerator(42)
		queries := qg.GenerateQueries(domain, 1, 50)
		if len(queries) == 0 {
			t.Errorf("domain %s produced no queries", domain.Name)
		}
	}
}

func TestQueryGenerator_QueryTypes(t *testing.T) {
	domain := Archetypes()[0]
	qg := NewQueryGenerator(42)

	queries := qg.GenerateQueries(domain, 1, 1000)

	expectedTypes := []string{
		"select_star", "select_columns", "non_sargable", "sargable",
		"unbounded_sort", "bounded_sort", "group_by",
		"window_no_partition", "exists_subquery", "proper_join",
		"distinct_join", "cte", "cartesian",
	}

	queryTypes := make(map[string]bool)
	for _, q := range queries {
		queryTypes[q.QueryType] = true
	}

	for _, expected := range expectedTypes {
		if !queryTypes[expected] {
			t.Errorf("missing query type: %s (have: %v)", expected, queryTypes)
		}
	}
}

func TestQueryGenerator_RuleCoverage(t *testing.T) {
	domain := Archetypes()[0]
	qg := NewQueryGenerator(42)

	queries := qg.GenerateQueries(domain, 1, 2000)

	rulesSeen := make(map[string]bool)
	for _, q := range queries {
		for _, r := range q.TargetRules {
			rulesSeen[r] = true
		}
	}

	// Should cover most rules
	expectedRules := []string{
		"select-star", "non-sargable", "unbounded-sort",
		"group-by-fanout", "window-function", "correlated-subquery",
		"distinct-dedup", "cartesian-product", "missing-predicate",
		"set-operation", "cte", "boolean-nesting", "case-expression",
	}

	for _, rule := range expectedRules {
		if !rulesSeen[rule] {
			t.Errorf("rule %s not covered by any generated query", rule)
		}
	}
}

func TestQueryGenerator_ValidSQL(t *testing.T) {
	domain := Archetypes()[0]
	qg := NewQueryGenerator(42)

	queries := qg.GenerateQueries(domain, 1, 100)

	for _, q := range queries {
		sql := strings.TrimSpace(q.SQL)
		if sql == "" {
			t.Error("empty SQL")
			continue
		}
		// Basic validation: should start with SELECT or WITH
		upper := strings.ToUpper(sql)
		if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
			t.Errorf("unexpected SQL prefix: %s", sql[:min(50, len(sql))])
		}
	}
}

func TestNonsargableFunc(t *testing.T) {
	tests := []struct {
		colType  string
		wantFunc string
	}{
		{"VARCHAR(100)", ""},
		{"TEXT", ""},
		{"INT", "ABS"},
		{"NUMERIC(10,2)", ""},
		{"DATE", "DATE_TRUNC"},
		{"TIMESTAMPTZ", "DATE_TRUNC"},
	}

	for _, tt := range tests {
		got := nonsargableFunc(tt.colType)
		if tt.wantFunc != "" && got != tt.wantFunc {
			t.Errorf("nonsargableFunc(%q) = %q, want %q", tt.colType, got, tt.wantFunc)
		}
		if got == "" {
			t.Errorf("nonsargableFunc(%q) returned empty", tt.colType)
		}
	}
}

func TestSampleValue(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	types := []string{"VARCHAR(100)", "TEXT", "INT", "BIGINT", "NUMERIC(10,2)", "BOOLEAN", "DATE", "TIMESTAMPTZ", "JSONB"}
	for _, typ := range types {
		val := sampleValue(typ, rng)
		if val == "" {
			t.Errorf("sampleValue(%q) returned empty", typ)
		}
	}
}

func TestMin(t *testing.T) {
	if min(3, 5) != 3 {
		t.Error("min(3,5) != 3")
	}
	if min(7, 2) != 2 {
		t.Error("min(7,2) != 2")
	}
	if min(4, 4) != 4 {
		t.Error("min(4,4) != 4")
	}
}

func TestQueryGenerator_EmptyDomain(t *testing.T) {
	qg := NewQueryGenerator(42)
	domain := Domain{Name: "empty", Tables: nil}
	queries := qg.GenerateQueries(domain, 1, 10)
	if len(queries) != 0 {
		t.Errorf("empty domain should produce no queries, got %d", len(queries))
	}
}

func TestQueryGenerator_Deterministic(t *testing.T) {
	domain := Archetypes()[0]

	qg1 := NewQueryGenerator(42)
	queries1 := qg1.GenerateQueries(domain, 1, 10)

	qg2 := NewQueryGenerator(42)
	queries2 := qg2.GenerateQueries(domain, 1, 10)

	// Same seed should produce same query types (SQL may vary if closures capture non-deterministic state)
	for i := range queries1 {
		if queries1[i].QueryType != queries2[i].QueryType {
			t.Errorf("query %d type differs: %s vs %s", i, queries1[i].QueryType, queries2[i].QueryType)
			break
		}
	}
}
