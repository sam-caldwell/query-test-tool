package scorer

import (
	"testing"

	"github.com/sam-caldwell/query-test-tool/parser"
)

func parseOrFatal(t *testing.T, sql string) {
	t.Helper()
	_, err := parser.Parse(sql)
	if err != nil {
		t.Fatalf("failed to parse SQL: %v", err)
	}
}

func TestEfficiency_SelectStar(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		wantRule string
		wantHit  bool
	}{
		{"select star", "SELECT * FROM users", "select-star", true},
		{"select columns", "SELECT id, name FROM users", "select-star", false},
		{"select star with join", "SELECT * FROM users u JOIN orders o ON u.id = o.user_id", "select-star", true},
		{"select count star", "SELECT count(*) FROM users", "select-star", false},
		{"select qualified star", "SELECT u.* FROM users u", "select-star", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			hit := hasRule(report.Efficiency.Findings, tt.wantRule)
			if hit != tt.wantHit {
				t.Errorf("rule %q: got hit=%v, want %v (score=%d, findings=%v)",
					tt.wantRule, hit, tt.wantHit, report.Efficiency.Score, report.Efficiency.Findings)
			}
		})
	}
}

func TestEfficiency_MissingPredicate(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{"two tables no where", "SELECT * FROM users, orders", true},
		{"two tables with where", "SELECT * FROM users, orders WHERE users.id = orders.user_id", false},
		{"single table no where", "SELECT * FROM users", false},
		{"join with on", "SELECT * FROM users u JOIN orders o ON u.id = o.user_id", false},
		{"three tables no where", "SELECT * FROM a, b, c", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			hit := hasRule(report.Efficiency.Findings, "missing-predicate")
			if hit != tt.wantHit {
				t.Errorf("missing-predicate: got hit=%v, want %v", hit, tt.wantHit)
			}
		})
	}
}

func TestEfficiency_CorrelatedSubquery(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"exists with correlation",
			"SELECT * FROM users WHERE EXISTS (SELECT 1 FROM orders WHERE orders.user_id = users.id)",
			true,
		},
		{
			"scalar subquery no correlation",
			"SELECT *, (SELECT count(*) FROM orders) FROM users",
			false, // uncorrelated scalar subquery
		},
		{
			"in subquery",
			"SELECT * FROM users WHERE id IN (SELECT user_id FROM orders)",
			true,
		},
		{
			"simple select",
			"SELECT * FROM users WHERE id = 1",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			hit := hasRule(report.Efficiency.Findings, "correlated-subquery")
			if hit != tt.wantHit {
				t.Errorf("correlated-subquery: got hit=%v, want %v (findings=%v)", hit, tt.wantHit, report.Efficiency.Findings)
			}
		})
	}
}

func TestEfficiency_NonSargable(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{"lower on column", "SELECT * FROM users WHERE LOWER(email) = 'test@test.com'", true},
		{"upper on column", "SELECT * FROM users WHERE UPPER(name) = 'BOB'", true},
		{"trim on column", "SELECT * FROM users WHERE TRIM(name) = 'bob'", true},
		{"date_trunc on column", "SELECT * FROM events WHERE date_trunc('day', created_at) = '2024-01-01'", true},
		{"no function", "SELECT * FROM users WHERE email = 'test@test.com'", false},
		{"function on constant", "SELECT * FROM users WHERE email = LOWER('TEST@TEST.COM')", false},
		{"substr on column", "SELECT * FROM users WHERE substr(name, 1, 3) = 'Bob'", true},
		{"abs on column", "SELECT * FROM users WHERE abs(balance) > 100", true},
		{"round on column", "SELECT * FROM users WHERE round(score) = 5", true},
		{"extract on column", "SELECT * FROM events WHERE extract(year FROM created_at) = 2024", true},
		{"length on column", "SELECT * FROM users WHERE length(name) > 10", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			hit := hasRule(report.Efficiency.Findings, "non-sargable")
			if hit != tt.wantHit {
				t.Errorf("non-sargable: got hit=%v, want %v (findings=%v)", hit, tt.wantHit, report.Efficiency.Findings)
			}
		})
	}
}

func TestEfficiency_DistinctDedup(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"distinct with join",
			"SELECT DISTINCT u.id FROM users u JOIN orders o ON u.id = o.user_id",
			true,
		},
		{
			"distinct without join",
			"SELECT DISTINCT name FROM users",
			false,
		},
		{
			"no distinct with join",
			"SELECT u.id FROM users u JOIN orders o ON u.id = o.user_id",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			hit := hasRule(report.Efficiency.Findings, "distinct-dedup")
			if hit != tt.wantHit {
				t.Errorf("distinct-dedup: got hit=%v, want %v", hit, tt.wantHit)
			}
		})
	}
}

func TestEfficiency_MultipleFindings(t *testing.T) {
	// This query should trigger multiple efficiency findings
	sql := "SELECT DISTINCT * FROM users u JOIN orders o ON u.id = o.user_id WHERE LOWER(u.email) = 'test'"
	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Efficiency.Findings) < 3 {
		t.Errorf("expected at least 3 findings, got %d: %v", len(report.Efficiency.Findings), report.Efficiency.Findings)
	}

	expectedRules := []string{"select-star", "non-sargable", "distinct-dedup"}
	for _, rule := range expectedRules {
		if !hasRule(report.Efficiency.Findings, rule) {
			t.Errorf("expected rule %q in findings", rule)
		}
	}
}

func TestEfficiency_CleanQuery(t *testing.T) {
	sql := "SELECT id, name FROM users WHERE id = 1"
	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatal(err)
	}
	if report.Efficiency.Score != 0 {
		t.Errorf("clean query should have efficiency score 0, got %d", report.Efficiency.Score)
	}
}

func TestEfficiency_Penalties(t *testing.T) {
	// Verify each penalty value
	sql := "SELECT * FROM users"
	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatal(err)
	}
	if report.Efficiency.Score != PenaltySelectStar() {
		t.Errorf("select star penalty: got %d, want %d", report.Efficiency.Score, PenaltySelectStar())
	}
}

func TestContainsColumnRef(t *testing.T) {
	// Test via a query with a function wrapping a column ref
	sql := "SELECT * FROM users WHERE LOWER(email) = 'test'"
	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRule(report.Efficiency.Findings, "non-sargable") {
		t.Error("expected non-sargable finding for LOWER(column)")
	}
}

func hasRule(findings []Finding, rule string) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}
