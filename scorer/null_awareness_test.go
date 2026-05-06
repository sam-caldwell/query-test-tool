package scorer

import (
	"testing"

	"github.com/sam-caldwell/query-test-tool/parser"
)

func TestNullCoalesceInPredicate(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"coalesce in WHERE",
			"SELECT * FROM users WHERE COALESCE(age, 0) > 5",
			true,
		},
		{
			"coalesce in JOIN ON",
			"SELECT * FROM a JOIN b ON COALESCE(a.x, 0) = b.x",
			true,
		},
		{
			"coalesce on constant only",
			"SELECT * FROM users WHERE age > COALESCE(5, 0)",
			false,
		},
		{
			"no coalesce",
			"SELECT * FROM users WHERE age > 5",
			false,
		},
		{
			"coalesce in SELECT list not in predicate",
			"SELECT COALESCE(name, 'unknown') FROM users WHERE id = 1",
			false,
		},
		{
			"multiple coalesce in WHERE",
			"SELECT * FROM users WHERE COALESCE(a, 0) > 1 AND COALESCE(b, 0) < 10",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("failed to parse SQL: %v", err)
			}
			ds := DimensionScore{Name: "efficiency"}
			scoreNullPatterns(tree, &ds, &ds)
			hit := hasRule(ds.Findings, "null-coalesce-in-predicate")
			if hit != tt.wantHit {
				t.Errorf("null-coalesce-in-predicate: got hit=%v, want %v (findings=%v)",
					hit, tt.wantHit, ds.Findings)
			}
		})
	}
}

func TestNullCheckChain(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"three null checks triggers",
			"SELECT * FROM t WHERE a IS NULL AND b IS NOT NULL AND c IS NULL",
			true,
		},
		{
			"single null check does not trigger",
			"SELECT * FROM t WHERE col IS NULL",
			false,
		},
		{
			"two null checks does not trigger",
			"SELECT * FROM t WHERE a IS NULL AND b IS NOT NULL",
			false,
		},
		{
			"no null checks",
			"SELECT * FROM t WHERE a = 1 AND b = 2",
			false,
		},
		{
			"four null checks triggers",
			"SELECT * FROM t WHERE a IS NULL AND b IS NULL AND c IS NOT NULL AND d IS NULL",
			true,
		},
		{
			"null checks in JOIN ON clause",
			"SELECT * FROM a JOIN b ON a.id = b.id AND a.x IS NULL AND b.y IS NULL AND a.z IS NOT NULL",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("failed to parse SQL: %v", err)
			}
			ds := DimensionScore{Name: "cognitive_complexity"}
			scoreNullPatterns(tree, &ds, &ds)
			hit := hasRule(ds.Findings, "null-check-chain")
			if hit != tt.wantHit {
				t.Errorf("null-check-chain: got hit=%v, want %v (findings=%v)",
					hit, tt.wantHit, ds.Findings)
			}
		})
	}
}

func TestNullPatterns_CleanQuery(t *testing.T) {
	sql := "SELECT id, name FROM users WHERE id = 1"
	tree, err := parser.Parse(sql)
	if err != nil {
		t.Fatal(err)
	}
	ds := DimensionScore{Name: "efficiency"}
	scoreNullPatterns(tree, &ds, &ds)
	if ds.Score != 0 {
		t.Errorf("clean query should have score 0, got %d", ds.Score)
	}
	if len(ds.Findings) != 0 {
		t.Errorf("clean query should have no findings, got %v", ds.Findings)
	}
}

func TestNullPatterns_NilHandling(t *testing.T) {
	// Verify nil node handling doesn't panic.
	eff := DimensionScore{Name: "efficiency"}
	cog := DimensionScore{Name: "cognitive_complexity"}
	scoreNullNode(nil, &eff, &cog)
	if eff.Score != 0 || cog.Score != 0 {
		t.Error("nil node should produce no findings")
	}

	checkCoalesceInPredicate(nil, &eff)
	if eff.Score != 0 {
		t.Error("nil SelectStmt should produce no findings")
	}

	checkNullCheckChain(nil, &cog)
	if cog.Score != 0 {
		t.Error("nil SelectStmt should produce no findings")
	}
}

func TestNullCoalesceInPredicate_MultipleFindings(t *testing.T) {
	sql := "SELECT * FROM users WHERE COALESCE(a, 0) > 1 AND COALESCE(b, 0) < 10"
	tree, err := parser.Parse(sql)
	if err != nil {
		t.Fatal(err)
	}
	ds := DimensionScore{Name: "efficiency"}
	scoreNullPatterns(tree, &ds, &ds)
	count := countRule(ds.Findings, "null-coalesce-in-predicate")
	if count < 2 {
		t.Errorf("expected at least 2 null-coalesce-in-predicate findings, got %d", count)
	}
}
