package scorer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sam-caldwell/query-test-tool/src/parser"
)

func TestMissingWhereClause(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"UPDATE without WHERE",
			"UPDATE users SET active = false",
			true,
		},
		{
			"UPDATE with WHERE",
			"UPDATE users SET active = false WHERE id = 1",
			false,
		},
		{
			"DELETE without WHERE",
			"DELETE FROM users",
			true,
		},
		{
			"DELETE with WHERE",
			"DELETE FROM users WHERE id = 1",
			false,
		},
		{
			"SELECT without WHERE does not trigger",
			"SELECT * FROM users",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("failed to parse SQL: %v", err)
			}
			eff := DimensionScore{Name: "efficiency"}
			mem := DimensionScore{Name: "memory_compute"}
			cog := DimensionScore{Name: "cognitive_complexity"}
			scoreDMLPatterns(tree, &eff, &mem, &cog)
			hit := hasRule(eff.Findings, "missing-where-clause")
			if hit != tt.wantHit {
				t.Errorf("missing-where-clause: got hit=%v, want %v (findings=%v)",
					hit, tt.wantHit, eff.Findings)
			}
		})
	}
}

func TestLargeOffset(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"OFFSET > 100 triggers",
			"SELECT * FROM users ORDER BY id LIMIT 10 OFFSET 500",
			true,
		},
		{
			"OFFSET = 100 does not trigger",
			"SELECT * FROM users ORDER BY id LIMIT 10 OFFSET 100",
			false,
		},
		{
			"OFFSET = 10 does not trigger",
			"SELECT * FROM users ORDER BY id LIMIT 10 OFFSET 10",
			false,
		},
		{
			"no OFFSET does not trigger",
			"SELECT * FROM users ORDER BY id LIMIT 10",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("failed to parse SQL: %v", err)
			}
			eff := DimensionScore{Name: "efficiency"}
			mem := DimensionScore{Name: "memory_compute"}
			cog := DimensionScore{Name: "cognitive_complexity"}
			scoreDMLPatterns(tree, &eff, &mem, &cog)
			hit := hasRule(eff.Findings, "large-offset")
			if hit != tt.wantHit {
				t.Errorf("large-offset: got hit=%v, want %v (findings=%v)",
					hit, tt.wantHit, eff.Findings)
			}
		})
	}
}

func TestRecursiveCTE(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"WITH RECURSIVE triggers",
			"WITH RECURSIVE cte AS (SELECT 1 UNION ALL SELECT n+1 FROM cte WHERE n < 10) SELECT * FROM cte",
			true,
		},
		{
			"regular CTE does not trigger",
			"WITH cte AS (SELECT 1 AS n) SELECT * FROM cte",
			false,
		},
		{
			"no CTE does not trigger",
			"SELECT * FROM users WHERE id = 1",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("failed to parse SQL: %v", err)
			}
			eff := DimensionScore{Name: "efficiency"}
			mem := DimensionScore{Name: "memory_compute"}
			cog := DimensionScore{Name: "cognitive_complexity"}
			scoreDMLPatterns(tree, &eff, &mem, &cog)
			hit := hasRule(mem.Findings, "recursive-cte")
			if hit != tt.wantHit {
				t.Errorf("recursive-cte: got hit=%v, want %v (findings=%v)",
					hit, tt.wantHit, mem.Findings)
			}
		})
	}
}

func TestLargeInList(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"IN list with 25 values triggers",
			"SELECT * FROM users WHERE id IN (" + generateInList(25) + ")",
			true,
		},
		{
			"IN list with 5 values does not trigger",
			"SELECT * FROM users WHERE id IN (1, 2, 3, 4, 5)",
			false,
		},
		{
			"IN list with 20 values does not trigger (at threshold)",
			"SELECT * FROM users WHERE id IN (" + generateInList(20) + ")",
			false,
		},
		{
			"no IN clause",
			"SELECT * FROM users WHERE id = 1",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("failed to parse SQL: %v", err)
			}
			eff := DimensionScore{Name: "efficiency"}
			mem := DimensionScore{Name: "memory_compute"}
			cog := DimensionScore{Name: "cognitive_complexity"}
			scoreDMLPatterns(tree, &eff, &mem, &cog)
			hit := hasRule(eff.Findings, "large-in-list")
			if hit != tt.wantHit {
				t.Errorf("large-in-list: got hit=%v, want %v (findings=%v)",
					hit, tt.wantHit, eff.Findings)
			}
		})
	}
}

func TestLikeLeadingWildcard(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"LIKE with leading wildcard triggers",
			"SELECT * FROM users WHERE name LIKE '%smith'",
			true,
		},
		{
			"ILIKE with leading wildcard triggers",
			"SELECT * FROM users WHERE name ILIKE '%smith'",
			true,
		},
		{
			"LIKE with trailing wildcard does not trigger",
			"SELECT * FROM users WHERE name LIKE 'smith%'",
			false,
		},
		{
			"LIKE with middle wildcard does not trigger",
			"SELECT * FROM users WHERE name LIKE 'sm%th'",
			false,
		},
		{
			"no LIKE",
			"SELECT * FROM users WHERE name = 'smith'",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("failed to parse SQL: %v", err)
			}
			eff := DimensionScore{Name: "efficiency"}
			mem := DimensionScore{Name: "memory_compute"}
			cog := DimensionScore{Name: "cognitive_complexity"}
			scoreDMLPatterns(tree, &eff, &mem, &cog)
			hit := hasRule(eff.Findings, "like-leading-wildcard")
			if hit != tt.wantHit {
				t.Errorf("like-leading-wildcard: got hit=%v, want %v (findings=%v)",
					hit, tt.wantHit, eff.Findings)
			}
		})
	}
}

func TestImplicitCastInPredicate(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"column::text cast triggers",
			"SELECT * FROM users WHERE id::text = '123'",
			true,
		},
		{
			"no cast does not trigger",
			"SELECT * FROM users WHERE id = 123",
			false,
		},
		{
			"cast on constant does not trigger",
			"SELECT * FROM users WHERE id = '123'::int",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("failed to parse SQL: %v", err)
			}
			eff := DimensionScore{Name: "efficiency"}
			mem := DimensionScore{Name: "memory_compute"}
			cog := DimensionScore{Name: "cognitive_complexity"}
			scoreDMLPatterns(tree, &eff, &mem, &cog)
			hit := hasRule(eff.Findings, "implicit-cast-in-predicate")
			if hit != tt.wantHit {
				t.Errorf("implicit-cast-in-predicate: got hit=%v, want %v (findings=%v)",
					hit, tt.wantHit, eff.Findings)
			}
		})
	}
}

func TestLateralJoin(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"LATERAL subquery triggers",
			"SELECT * FROM users u, LATERAL (SELECT * FROM orders o WHERE o.user_id = u.id LIMIT 3) sub",
			true,
		},
		{
			"regular subquery in FROM does not trigger",
			"SELECT * FROM users u, (SELECT * FROM orders LIMIT 3) sub",
			false,
		},
		{
			"no subquery does not trigger",
			"SELECT * FROM users WHERE id = 1",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("failed to parse SQL: %v", err)
			}
			eff := DimensionScore{Name: "efficiency"}
			mem := DimensionScore{Name: "memory_compute"}
			cog := DimensionScore{Name: "cognitive_complexity"}
			scoreDMLPatterns(tree, &eff, &mem, &cog)
			hit := hasRule(mem.Findings, "lateral-join")
			if hit != tt.wantHit {
				t.Errorf("lateral-join: got hit=%v, want %v (findings=%v)",
					hit, tt.wantHit, mem.Findings)
			}
		})
	}
}

func TestReturningClause(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"INSERT RETURNING triggers",
			"INSERT INTO users (name) VALUES ('test') RETURNING id",
			true,
		},
		{
			"UPDATE RETURNING triggers",
			"UPDATE users SET name = 'test' WHERE id = 1 RETURNING *",
			true,
		},
		{
			"DELETE RETURNING triggers",
			"DELETE FROM users WHERE id = 1 RETURNING id",
			true,
		},
		{
			"INSERT without RETURNING does not trigger",
			"INSERT INTO users (name) VALUES ('test')",
			false,
		},
		{
			"UPDATE without RETURNING does not trigger",
			"UPDATE users SET name = 'test' WHERE id = 1",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("failed to parse SQL: %v", err)
			}
			eff := DimensionScore{Name: "efficiency"}
			mem := DimensionScore{Name: "memory_compute"}
			cog := DimensionScore{Name: "cognitive_complexity"}
			scoreDMLPatterns(tree, &eff, &mem, &cog)
			hit := hasRule(cog.Findings, "returning-clause")
			if hit != tt.wantHit {
				t.Errorf("returning-clause: got hit=%v, want %v (findings=%v)",
					hit, tt.wantHit, cog.Findings)
			}
		})
	}
}

func TestGroupingSets(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"ROLLUP triggers",
			"SELECT department, year, SUM(sales) FROM sales GROUP BY ROLLUP(department, year)",
			true,
		},
		{
			"CUBE triggers",
			"SELECT department, year, SUM(sales) FROM sales GROUP BY CUBE(department, year)",
			true,
		},
		{
			"GROUPING SETS triggers",
			"SELECT department, year, SUM(sales) FROM sales GROUP BY GROUPING SETS ((department), (year), ())",
			true,
		},
		{
			"regular GROUP BY does not trigger",
			"SELECT department, SUM(sales) FROM sales GROUP BY department",
			false,
		},
		{
			"no GROUP BY does not trigger",
			"SELECT * FROM users WHERE id = 1",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("failed to parse SQL: %v", err)
			}
			eff := DimensionScore{Name: "efficiency"}
			mem := DimensionScore{Name: "memory_compute"}
			cog := DimensionScore{Name: "cognitive_complexity"}
			scoreDMLPatterns(tree, &eff, &mem, &cog)
			hit := hasRule(mem.Findings, "grouping-sets")
			if hit != tt.wantHit {
				t.Errorf("grouping-sets: got hit=%v, want %v (findings=%v)",
					hit, tt.wantHit, mem.Findings)
			}
		})
	}
}

func TestForUpdateLock(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"FOR UPDATE triggers",
			"SELECT * FROM users WHERE id = 1 FOR UPDATE",
			true,
		},
		{
			"FOR SHARE triggers",
			"SELECT * FROM users WHERE id = 1 FOR SHARE",
			true,
		},
		{
			"no locking does not trigger",
			"SELECT * FROM users WHERE id = 1",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("failed to parse SQL: %v", err)
			}
			eff := DimensionScore{Name: "efficiency"}
			mem := DimensionScore{Name: "memory_compute"}
			cog := DimensionScore{Name: "cognitive_complexity"}
			scoreDMLPatterns(tree, &eff, &mem, &cog)
			hit := hasRule(eff.Findings, "for-update-lock")
			if hit != tt.wantHit {
				t.Errorf("for-update-lock: got hit=%v, want %v (findings=%v)",
					hit, tt.wantHit, eff.Findings)
			}
		})
	}
}

func TestUnionDistinct(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"UNION triggers",
			"SELECT id FROM users UNION SELECT id FROM admins",
			true,
		},
		{
			"UNION ALL does not trigger",
			"SELECT id FROM users UNION ALL SELECT id FROM admins",
			false,
		},
		{
			"INTERSECT does not trigger union-distinct",
			"SELECT id FROM users INTERSECT SELECT id FROM admins",
			false,
		},
		{
			"no set operation does not trigger",
			"SELECT * FROM users WHERE id = 1",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("failed to parse SQL: %v", err)
			}
			eff := DimensionScore{Name: "efficiency"}
			mem := DimensionScore{Name: "memory_compute"}
			cog := DimensionScore{Name: "cognitive_complexity"}
			scoreDMLPatterns(tree, &eff, &mem, &cog)
			hit := hasRule(eff.Findings, "union-distinct")
			if hit != tt.wantHit {
				t.Errorf("union-distinct: got hit=%v, want %v (findings=%v)",
					hit, tt.wantHit, eff.Findings)
			}
		})
	}
}

func TestDMLPatterns_NilHandling(t *testing.T) {
	eff := DimensionScore{Name: "efficiency"}
	mem := DimensionScore{Name: "memory_compute"}
	cog := DimensionScore{Name: "cognitive_complexity"}
	scoreDMLNode(nil, &eff, &mem, &cog)
	if eff.Score != 0 || mem.Score != 0 || cog.Score != 0 {
		t.Error("nil node should produce no findings")
	}
}

func TestDMLPatterns_CleanQuery(t *testing.T) {
	sql := "SELECT id, name FROM users WHERE id = 1"
	tree, err := parser.Parse(sql)
	if err != nil {
		t.Fatal(err)
	}
	eff := DimensionScore{Name: "efficiency"}
	mem := DimensionScore{Name: "memory_compute"}
	cog := DimensionScore{Name: "cognitive_complexity"}
	scoreDMLPatterns(tree, &eff, &mem, &cog)
	if eff.Score != 0 {
		t.Errorf("clean query should have efficiency score 0, got %d", eff.Score)
	}
	if mem.Score != 0 {
		t.Errorf("clean query should have memory_compute score 0, got %d", mem.Score)
	}
	if cog.Score != 0 {
		t.Errorf("clean query should have cognitive score 0, got %d", cog.Score)
	}
}

// generateInList generates a comma-separated list of n integers.
func generateInList(n int) string {
	items := make([]string, n)
	for i := 0; i < n; i++ {
		items[i] = fmt.Sprintf("%d", i+1)
	}
	return strings.Join(items, ", ")
}
