package scorer

import (
	"testing"
)

func TestCognitive_Joins(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		wantJoins int
	}{
		{"no join", "SELECT * FROM users", 0},
		{"one join", "SELECT * FROM users u JOIN orders o ON u.id = o.user_id", 1},
		{"two joins", "SELECT * FROM a JOIN b ON a.id = b.a_id JOIN c ON b.id = c.b_id", 2},
		{"three joins", "SELECT * FROM a JOIN b ON a.id = b.a_id JOIN c ON b.id = c.b_id JOIN d ON c.id = d.c_id", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			joinCount := countRule(report.CognitiveComplex.Findings, "join")
			if joinCount != tt.wantJoins {
				t.Errorf("join count: got %d, want %d", joinCount, tt.wantJoins)
			}
		})
	}
}

func TestCognitive_SubqueryNesting(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"no subquery",
			"SELECT * FROM users WHERE id = 1",
			false,
		},
		{
			"one level subquery",
			"SELECT * FROM users WHERE id IN (SELECT user_id FROM orders)",
			true,
		},
		{
			"exists subquery",
			"SELECT * FROM users WHERE EXISTS (SELECT 1 FROM orders WHERE orders.user_id = users.id)",
			true,
		},
		{
			"derived table",
			"SELECT * FROM (SELECT id, name FROM users) AS sub",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			hit := hasRule(report.CognitiveComplex.Findings, "subquery-nesting")
			if hit != tt.wantHit {
				t.Errorf("subquery-nesting: got hit=%v, want %v (findings=%v)", hit, tt.wantHit, report.CognitiveComplex.Findings)
			}
		})
	}
}

func TestCognitive_SubqueryNestingDepthPenalty(t *testing.T) {
	// Deeper nesting should produce higher penalties
	shallow, _ := ScoreQuery("SELECT * FROM users WHERE id IN (SELECT user_id FROM orders)")
	deep, _ := ScoreQuery("SELECT * FROM users WHERE id IN (SELECT user_id FROM orders WHERE order_id IN (SELECT id FROM items))")

	if deep.CognitiveComplex.Score <= shallow.CognitiveComplex.Score {
		t.Errorf("deeper nesting (%d) should score higher than shallow (%d)",
			deep.CognitiveComplex.Score, shallow.CognitiveComplex.Score)
	}
}

func TestCognitive_BooleanNesting(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"simple where",
			"SELECT * FROM users WHERE id = 1",
			false,
		},
		{
			"flat AND",
			"SELECT * FROM users WHERE id = 1 AND name = 'bob'",
			false, // flat booleans at depth 0 don't trigger nesting
		},
		{
			"nested AND/OR",
			"SELECT * FROM users WHERE (id = 1 AND name = 'bob') OR status = 'active'",
			true,
		},
		{
			"deeply nested",
			"SELECT * FROM users WHERE ((a = 1 AND b = 2) OR (c = 3 AND d = 4))",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			hit := hasRule(report.CognitiveComplex.Findings, "boolean-nesting")
			if hit != tt.wantHit {
				t.Errorf("boolean-nesting: got hit=%v, want %v (findings=%v)", hit, tt.wantHit, report.CognitiveComplex.Findings)
			}
		})
	}
}

func TestCognitive_CTE(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		wantCount int
	}{
		{"no CTE", "SELECT * FROM users", 0},
		{"one CTE", "WITH cte AS (SELECT 1) SELECT * FROM cte", 1},
		{"two CTEs", "WITH a AS (SELECT 1), b AS (SELECT 2) SELECT * FROM a, b", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			count := countRule(report.CognitiveComplex.Findings, "cte")
			if count != tt.wantCount {
				t.Errorf("CTE count: got %d, want %d", count, tt.wantCount)
			}
		})
	}
}

func TestCognitive_SetOperations(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{"union", "SELECT 1 UNION SELECT 2", true},
		{"union all", "SELECT 1 UNION ALL SELECT 2", true},
		{"intersect", "SELECT 1 INTERSECT SELECT 2", true},
		{"except", "SELECT 1 EXCEPT SELECT 2", true},
		{"no set op", "SELECT 1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			hit := hasRule(report.CognitiveComplex.Findings, "set-operation")
			if hit != tt.wantHit {
				t.Errorf("set-operation: got hit=%v, want %v", hit, tt.wantHit)
			}
		})
	}
}

func TestCognitive_CaseExpression(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"case in select",
			"SELECT CASE WHEN x > 0 THEN 'pos' ELSE 'neg' END FROM t",
			true,
		},
		{
			"no case",
			"SELECT id FROM users",
			false,
		},
		{
			"multiple cases",
			"SELECT CASE WHEN a = 1 THEN 'x' END, CASE WHEN b = 2 THEN 'y' END FROM t",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			hit := hasRule(report.CognitiveComplex.Findings, "case-expression")
			if hit != tt.wantHit {
				t.Errorf("case-expression: got hit=%v, want %v", hit, tt.wantHit)
			}
		})
	}
}

func TestCognitive_MultipleCases(t *testing.T) {
	sql := "SELECT CASE WHEN a = 1 THEN 'x' END, CASE WHEN b = 2 THEN 'y' END FROM t"
	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatal(err)
	}
	count := countRule(report.CognitiveComplex.Findings, "case-expression")
	if count < 2 {
		t.Errorf("expected at least 2 case-expression findings, got %d", count)
	}
}

func TestCognitive_CleanQuery(t *testing.T) {
	sql := "SELECT id FROM users WHERE id = 1"
	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatal(err)
	}
	if report.CognitiveComplex.Score != 0 {
		t.Errorf("clean query should have cognitive score 0, got %d", report.CognitiveComplex.Score)
	}
}

func TestCognitive_ComplexQuery(t *testing.T) {
	sql := `
		WITH recent_orders AS (
			SELECT user_id, count(*) as order_count
			FROM orders
			WHERE created_at > '2024-01-01'
			GROUP BY user_id
		)
		SELECT DISTINCT u.id, u.name,
			CASE WHEN ro.order_count > 10 THEN 'vip' ELSE 'regular' END as tier
		FROM users u
		JOIN recent_orders ro ON u.id = ro.user_id
		LEFT JOIN addresses a ON u.id = a.user_id
		WHERE (u.status = 'active' AND u.verified = true) OR u.role = 'admin'
		ORDER BY u.name
	`

	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatal(err)
	}

	if report.CognitiveComplex.Score == 0 {
		t.Error("complex query should have non-zero cognitive score")
	}

	// Should have findings for: CTE, joins (2), case, boolean nesting
	expectedRules := []string{"cte", "join", "case-expression"}
	for _, rule := range expectedRules {
		if !hasRule(report.CognitiveComplex.Findings, rule) {
			t.Errorf("expected rule %q in findings", rule)
		}
	}
}

func TestCognitive_HavingClauseBooleanNesting(t *testing.T) {
	sql := "SELECT dept, count(*) FROM employees GROUP BY dept HAVING (count(*) > 5 AND avg(salary) > 50000) OR dept = 'eng'"
	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatal(err)
	}
	hit := hasRule(report.CognitiveComplex.Findings, "boolean-nesting")
	if !hit {
		t.Error("expected boolean-nesting finding in HAVING clause")
	}
}

func TestCognitive_FromSubquery(t *testing.T) {
	sql := "SELECT sub.id FROM (SELECT id FROM users WHERE active = true) AS sub"
	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatal(err)
	}
	hit := hasRule(report.CognitiveComplex.Findings, "subquery-nesting")
	if !hit {
		t.Error("expected subquery-nesting finding for derived table in FROM")
	}
}

func TestCognitive_JoinWithFromSubquery(t *testing.T) {
	sql := "SELECT * FROM users u JOIN (SELECT user_id FROM orders) AS o ON u.id = o.user_id"
	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRule(report.CognitiveComplex.Findings, "join") {
		t.Error("expected join finding")
	}
	if !hasRule(report.CognitiveComplex.Findings, "subquery-nesting") {
		t.Error("expected subquery-nesting finding for derived table in JOIN")
	}
}

func countRule(findings []Finding, rule string) int {
	count := 0
	for _, f := range findings {
		if f.Rule == rule {
			count++
		}
	}
	return count
}
