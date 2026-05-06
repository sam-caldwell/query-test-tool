package scorer

import (
	"testing"

	"github.com/sam-caldwell/query-test-tool/parser"
)

func TestFunctionCost_ExpensiveTier1(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{"regexp_match", "SELECT regexp_match(col, '.*') FROM t", true},
		{"regexp_replace", "SELECT regexp_replace(col, 'a', 'b') FROM t", true},
		{"string_agg", "SELECT string_agg(name, ',') FROM t GROUP BY dept", true},
		{"array_agg", "SELECT array_agg(id) FROM t GROUP BY dept", true},
		{"to_tsvector", "SELECT to_tsvector('english', body) FROM t", true},
		{"ts_rank", "SELECT ts_rank(tsv, to_tsquery('test')) FROM t", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			ds := DimensionScore{Name: "efficiency"}
			scoreFunctionCost(tree, &ds)
			hit := hasRule(ds.Findings, "expensive-function")
			if hit != tt.wantHit {
				t.Errorf("expensive-function: got hit=%v, want %v (findings=%v)", hit, tt.wantHit, ds.Findings)
			}
		})
	}
}

func TestFunctionCost_ExpensiveTier2_ThresholdMet(t *testing.T) {
	// 4 distinct tier-2 functions: should trigger
	sql := "SELECT lower(a), upper(b), trim(c), concat(d, e) FROM t"
	tree, err := parser.Parse(sql)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ds := DimensionScore{Name: "efficiency"}
	scoreFunctionCost(tree, &ds)
	if !hasRule(ds.Findings, "expensive-function") {
		t.Errorf("expected expensive-function finding for 4 tier-2 functions, got %v", ds.Findings)
	}
}

func TestFunctionCost_ExpensiveTier2_BelowThreshold(t *testing.T) {
	// Only 1 tier-2 function: should NOT trigger
	sql := "SELECT lower(name) FROM t"
	tree, err := parser.Parse(sql)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ds := DimensionScore{Name: "efficiency"}
	scoreFunctionCost(tree, &ds)
	if hasRule(ds.Findings, "expensive-function") {
		t.Errorf("should not trigger expensive-function for 1 tier-2 function, got %v", ds.Findings)
	}
}

func TestFunctionCost_Volatile(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{"random in where", "SELECT * FROM t WHERE random() > 0.5", true},
		{"now in select", "SELECT now(), id FROM t", true},
		{"nextval", "SELECT nextval('my_seq')", true},
		{"clock_timestamp", "SELECT clock_timestamp()", true},
		{"no volatile", "SELECT id FROM t WHERE id = 1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			ds := DimensionScore{Name: "efficiency"}
			scoreFunctionCost(tree, &ds)
			hit := hasRule(ds.Findings, "volatile-function")
			if hit != tt.wantHit {
				t.Errorf("volatile-function: got hit=%v, want %v (findings=%v)", hit, tt.wantHit, ds.Findings)
			}
		})
	}
}

func TestFunctionCost_CleanQuery(t *testing.T) {
	sql := "SELECT id, name FROM t"
	tree, err := parser.Parse(sql)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ds := DimensionScore{Name: "efficiency"}
	scoreFunctionCost(tree, &ds)
	if len(ds.Findings) != 0 {
		t.Errorf("expected no findings for clean query, got %v", ds.Findings)
	}
	if ds.Score != 0 {
		t.Errorf("expected score 0 for clean query, got %d", ds.Score)
	}
}

func TestFunctionCost_MixedTiers(t *testing.T) {
	// Mix of tier-1 and tier-2: tier-1 always fires, tier-2 only if >= 3 distinct
	sql := "SELECT regexp_match(a, '.*'), lower(b) FROM t"
	tree, err := parser.Parse(sql)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ds := DimensionScore{Name: "efficiency"}
	scoreFunctionCost(tree, &ds)

	// Should have exactly 1 finding for the tier-1 function
	count := 0
	for _, f := range ds.Findings {
		if f.Rule == "expensive-function" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 expensive-function finding, got %d (findings=%v)", count, ds.Findings)
	}
}

func TestFunctionCost_VolatileAndExpensive(t *testing.T) {
	// Both volatile and expensive in same query
	sql := "SELECT regexp_match(col, '.*'), random() FROM t"
	tree, err := parser.Parse(sql)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ds := DimensionScore{Name: "efficiency"}
	scoreFunctionCost(tree, &ds)

	if !hasRule(ds.Findings, "expensive-function") {
		t.Error("expected expensive-function finding")
	}
	if !hasRule(ds.Findings, "volatile-function") {
		t.Error("expected volatile-function finding")
	}
}
