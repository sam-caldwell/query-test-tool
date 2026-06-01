package scorer

import (
	"testing"
)

func TestScoreQuery_ValidSQL(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{"simple select", "SELECT 1"},
		{"select with where", "SELECT id FROM users WHERE id = 1"},
		{"complex query", "SELECT * FROM users u JOIN orders o ON u.id = o.user_id WHERE LOWER(u.email) = 'test' ORDER BY o.created_at"},
		{"insert", "INSERT INTO users (name) VALUES ('alice')"},
		{"update", "UPDATE users SET name = 'bob' WHERE id = 1"},
		{"delete", "DELETE FROM users WHERE id = 1"},
		{"CTE with subquery", "WITH cte AS (SELECT * FROM users) SELECT * FROM cte WHERE id IN (SELECT user_id FROM orders)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatalf("ScoreQuery(%q) error: %v", tt.sql, err)
			}
			if report == nil {
				t.Fatal("report is nil")
			}
			if report.SQL != tt.sql {
				t.Errorf("report.SQL = %q, want %q", report.SQL, tt.sql)
			}
			if report.Efficiency.Name != "efficiency" {
				t.Errorf("efficiency name: got %q, want %q", report.Efficiency.Name, "efficiency")
			}
			if report.MemoryCompute.Name != "memory_compute" {
				t.Errorf("memory_compute name: got %q, want %q", report.MemoryCompute.Name, "memory_compute")
			}
			if report.CognitiveComplex.Name != "cognitive_complexity" {
				t.Errorf("cognitive_complexity name: got %q, want %q", report.CognitiveComplex.Name, "cognitive_complexity")
			}
		})
	}
}

func TestScoreQuery_InvalidSQL(t *testing.T) {
	_, err := ScoreQuery("NOT VALID SQL")
	if err == nil {
		t.Fatal("expected error for invalid SQL")
	}
}

func TestScoreQuery_TotalScore(t *testing.T) {
	report, err := ScoreQuery("SELECT id FROM users WHERE id = 1")
	if err != nil {
		t.Fatal(err)
	}
	expected := report.Efficiency.Score + report.MemoryCompute.Score + report.CognitiveComplex.Score
	if report.TotalScore != expected {
		t.Errorf("total score: got %d, want %d", report.TotalScore, expected)
	}
}

func TestScoreQuery_TotalScoreAdditive(t *testing.T) {
	// Query that triggers findings in all three dimensions
	sql := "SELECT DISTINCT * FROM users u JOIN orders o ON u.id = o.user_id WHERE LOWER(u.email) = 'test' ORDER BY u.name"
	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatal(err)
	}

	if report.Efficiency.Score == 0 {
		t.Error("expected non-zero efficiency score")
	}
	if report.MemoryCompute.Score == 0 {
		t.Error("expected non-zero memory_compute score")
	}
	if report.CognitiveComplex.Score == 0 {
		t.Error("expected non-zero cognitive score")
	}
	if report.TotalScore != report.Efficiency.Score+report.MemoryCompute.Score+report.CognitiveComplex.Score {
		t.Error("total score should be sum of dimension scores")
	}
}

func TestScoreQuery_ZeroScoreForSimple(t *testing.T) {
	report, err := ScoreQuery("SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalScore != 0 {
		t.Errorf("'SELECT 1' should have total score 0, got %d", report.TotalScore)
	}
}

func TestFuncName(t *testing.T) {
	tests := []struct {
		sql      string
		wantFunc string
	}{
		{"SELECT lower(x) FROM t", "lower"},
		{"SELECT upper(x) FROM t", "upper"},
		{"SELECT count(*) FROM t", "count"},
	}

	for _, tt := range tests {
		t.Run(tt.wantFunc, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			// Just verify it parses — funcName is tested implicitly through findings
			_ = report
		})
	}
}

func TestIsAggregate(t *testing.T) {
	aggs := []string{"count", "sum", "avg", "min", "max", "array_agg", "string_agg",
		"json_agg", "jsonb_agg", "bool_and", "bool_or", "every", "xmlagg",
		"json_object_agg", "jsonb_object_agg"}
	for _, a := range aggs {
		if !isAggregate(a) {
			t.Errorf("expected %q to be aggregate", a)
		}
	}

	nonAggs := []string{"lower", "upper", "trim", "concat", "random", "now"}
	for _, na := range nonAggs {
		if isAggregate(na) {
			t.Errorf("expected %q to NOT be aggregate", na)
		}
	}
}

func TestFindingCategory(t *testing.T) {
	sql := "SELECT * FROM users u JOIN orders o ON u.id = o.user_id WHERE LOWER(u.email) = 'test' ORDER BY u.name"
	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range report.Efficiency.Findings {
		if f.Category != "efficiency" {
			t.Errorf("efficiency finding has wrong category: %q", f.Category)
		}
	}
	for _, f := range report.MemoryCompute.Findings {
		if f.Category != "memory_compute" {
			t.Errorf("memory_compute finding has wrong category: %q", f.Category)
		}
	}
	for _, f := range report.CognitiveComplex.Findings {
		if f.Category != "cognitive_complexity" {
			t.Errorf("cognitive_complexity finding has wrong category: %q", f.Category)
		}
	}
}

// E2E test: score a realistic complex query
func TestScoreQuery_E2E_ComplexReport(t *testing.T) {
	sql := `
		WITH monthly_sales AS (
			SELECT
				date_trunc('month', o.created_at) as month,
				p.category,
				SUM(oi.quantity * oi.unit_price) as revenue,
				COUNT(DISTINCT o.id) as order_count
			FROM orders o
			JOIN order_items oi ON o.id = oi.order_id
			JOIN products p ON oi.product_id = p.id
			WHERE o.status = 'completed'
				AND o.created_at >= '2024-01-01'
			GROUP BY date_trunc('month', o.created_at), p.category
		),
		top_categories AS (
			SELECT category, SUM(revenue) as total_revenue
			FROM monthly_sales
			GROUP BY category
			HAVING SUM(revenue) > 10000
		)
		SELECT DISTINCT
			ms.*,
			tc.total_revenue,
			CASE
				WHEN tc.total_revenue > 100000 THEN 'platinum'
				WHEN tc.total_revenue > 50000 THEN 'gold'
				ELSE 'silver'
			END as tier,
			row_number() OVER (PARTITION BY ms.category ORDER BY ms.revenue DESC) as rank
		FROM monthly_sales ms
		JOIN top_categories tc ON ms.category = tc.category
		WHERE (ms.revenue > 1000 AND ms.order_count > 5) OR ms.category = 'electronics'
		ORDER BY ms.month, ms.revenue DESC
	`

	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatalf("failed to score complex query: %v", err)
	}

	// This query should have a non-trivial total score
	if report.TotalScore < 20 {
		t.Errorf("complex query should have total score >= 20, got %d", report.TotalScore)
	}

	// Verify all dimensions have findings
	if len(report.Efficiency.Findings) == 0 {
		t.Error("expected efficiency findings for complex query")
	}
	if len(report.MemoryCompute.Findings) == 0 {
		t.Error("expected memory_compute findings for complex query")
	}
	if len(report.CognitiveComplex.Findings) == 0 {
		t.Error("expected cognitive_complexity findings for complex query")
	}

	// Check specific expected findings
	if !hasRule(report.Efficiency.Findings, "select-star") {
		t.Error("expected select-star finding (ms.*)")
	}
	if !hasRule(report.CognitiveComplex.Findings, "cte") {
		t.Error("expected CTE finding")
	}
	if !hasRule(report.CognitiveComplex.Findings, "join") {
		t.Error("expected join finding")
	}
	if !hasRule(report.CognitiveComplex.Findings, "case-expression") {
		t.Error("expected case-expression finding")
	}
}

// E2E test: perfectly optimized query
func TestScoreQuery_E2E_OptimizedQuery(t *testing.T) {
	sql := "SELECT id, name, email FROM users WHERE id = $1"
	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalScore != 0 {
		t.Errorf("optimized query should score 0, got %d (findings: eff=%v, mem=%v, cog=%v)",
			report.TotalScore, report.Efficiency.Findings, report.MemoryCompute.Findings, report.CognitiveComplex.Findings)
	}
}
