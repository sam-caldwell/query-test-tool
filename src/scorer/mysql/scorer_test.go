package mysql

import (
	"testing"

	"github.com/sam-caldwell/query-test-tool/src/dialect"
	"github.com/sam-caldwell/query-test-tool/src/scorer"
)

func init() {
	// Ensure MySQL weights are active for these tests.
	scorer.SetDialect(dialect.MySQL)
}

func TestSelectStar(t *testing.T) {
	r, err := ScoreQuery("SELECT * FROM users")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.Efficiency.Findings, "select-star")
	if r.TotalScore == 0 {
		t.Error("SELECT * should have non-zero score")
	}
	if r.Dialect != "mysql" {
		t.Errorf("dialect = %q, want mysql", r.Dialect)
	}
}

func TestCleanQuery(t *testing.T) {
	r, err := ScoreQuery("SELECT id, name FROM users WHERE id = 1")
	if err != nil {
		t.Fatal(err)
	}
	if r.TotalScore != 0 {
		t.Errorf("clean query: got score %d, want 0", r.TotalScore)
	}
}

func TestNonSargable(t *testing.T) {
	r, err := ScoreQuery("SELECT id FROM users WHERE LOWER(email) = 'test'")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.Efficiency.Findings, "non-sargable")
}

func TestMissingPredicate(t *testing.T) {
	r, err := ScoreQuery("SELECT id FROM users")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.Efficiency.Findings, "missing-predicate")
}

func TestLeadingWildcard(t *testing.T) {
	r, err := ScoreQuery("SELECT id FROM users WHERE name LIKE '%test'")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.Efficiency.Findings, "like-leading-wildcard")
}

func TestDistinct(t *testing.T) {
	r, err := ScoreQuery("SELECT DISTINCT name FROM users")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.MemoryCompute.Findings, "distinct-dedup")
}

func TestUnboundedSort(t *testing.T) {
	r, err := ScoreQuery("SELECT id FROM users ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.MemoryCompute.Findings, "unbounded-sort")
}

func TestBoundedSort(t *testing.T) {
	r, err := ScoreQuery("SELECT id FROM users ORDER BY name LIMIT 10")
	if err != nil {
		t.Fatal(err)
	}
	assertNoFinding(t, r.MemoryCompute.Findings, "unbounded-sort")
}

func TestGroupBy(t *testing.T) {
	r, err := ScoreQuery("SELECT status, COUNT(*) FROM users GROUP BY status")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.MemoryCompute.Findings, "group-by-fanout")
}

func TestJoin(t *testing.T) {
	r, err := ScoreQuery("SELECT u.id FROM users u JOIN orders o ON u.id = o.user_id WHERE u.id = 1")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.CognitiveComplex.Findings, "join")
}

func TestMultipleJoins(t *testing.T) {
	r, err := ScoreQuery("SELECT u.id FROM users u JOIN orders o ON u.id = o.user_id JOIN items i ON o.id = i.order_id WHERE u.id = 1")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.CognitiveComplex.Findings, "join-escalation")
}

func TestOuterJoin(t *testing.T) {
	r, err := ScoreQuery("SELECT u.id FROM users u LEFT JOIN orders o ON u.id = o.user_id WHERE u.id = 1")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.CognitiveComplex.Findings, "outer-join")
}

func TestBooleanNesting(t *testing.T) {
	r, err := ScoreQuery("SELECT id FROM users WHERE (status = 'active' AND verified = 1) OR role = 'admin'")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.CognitiveComplex.Findings, "boolean-nesting")
}

func TestUpdateWithoutWhere(t *testing.T) {
	r, err := ScoreQuery("UPDATE users SET status = 'inactive'")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.Efficiency.Findings, "missing-where-clause")
}

func TestDeleteWithoutWhere(t *testing.T) {
	r, err := ScoreQuery("DELETE FROM users")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.Efficiency.Findings, "missing-where-clause")
}

func TestUpdateWithWhere(t *testing.T) {
	r, err := ScoreQuery("UPDATE users SET status = 'inactive' WHERE id = 1")
	if err != nil {
		t.Fatal(err)
	}
	assertNoFinding(t, r.Efficiency.Findings, "missing-where-clause")
}

func TestInvalidSQL(t *testing.T) {
	_, err := ScoreQuery("NOT VALID SQL AT ALL")
	if err == nil {
		t.Error("expected error for invalid SQL")
	}
}

func TestMySQLSpecificSyntax(t *testing.T) {
	// Backtick identifiers — MySQL-specific
	r, err := ScoreQuery("SELECT `id`, `name` FROM `users` WHERE `id` = 1")
	if err != nil {
		t.Fatal(err)
	}
	if r.TotalScore != 0 {
		t.Errorf("clean MySQL query: got score %d, want 0", r.TotalScore)
	}
}

func TestLargeOffset(t *testing.T) {
	r, err := ScoreQuery("SELECT id FROM users LIMIT 10 OFFSET 5000")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.Efficiency.Findings, "large-offset")
}

func TestSmallOffset(t *testing.T) {
	r, err := ScoreQuery("SELECT id FROM users LIMIT 10 OFFSET 100")
	if err != nil {
		t.Fatal(err)
	}
	assertNoFinding(t, r.Efficiency.Findings, "large-offset")
}

func TestSubquery(t *testing.T) {
	r, err := ScoreQuery("SELECT id FROM users WHERE id IN (SELECT user_id FROM orders)")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.CognitiveComplex.Findings, "subquery-nesting")
}

func TestCaseExpression(t *testing.T) {
	r, err := ScoreQuery("SELECT CASE WHEN status = 'active' THEN 1 ELSE 0 END AS flag FROM users WHERE id = 1")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.CognitiveComplex.Findings, "case-expression")
}

func TestDDLStatement(t *testing.T) {
	r, err := ScoreQuery("CREATE TABLE test (id INT)")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.CognitiveComplex.Findings, "ddl-statement")
}

func TestRightJoin(t *testing.T) {
	r, err := ScoreQuery("SELECT u.id FROM users u RIGHT JOIN orders o ON u.id = o.user_id WHERE o.id = 1")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.CognitiveComplex.Findings, "outer-join")
}

func TestAllDimensions(t *testing.T) {
	// A query that triggers findings in all three dimensions
	r, err := ScoreQuery("SELECT DISTINCT * FROM users u LEFT JOIN orders o ON u.id = o.user_id ORDER BY u.name")
	if err != nil {
		t.Fatal(err)
	}
	if r.Efficiency.Score == 0 {
		t.Error("expected non-zero efficiency score (select-star)")
	}
	if r.MemoryCompute.Score == 0 {
		t.Error("expected non-zero memory_compute score (distinct, unbounded sort)")
	}
	if r.CognitiveComplex.Score == 0 {
		t.Error("expected non-zero cognitive score (join, outer-join)")
	}
	if r.TotalScore != r.Efficiency.Score+r.MemoryCompute.Score+r.CognitiveComplex.Score {
		t.Errorf("total %d != sum of dimensions %d+%d+%d",
			r.TotalScore, r.Efficiency.Score, r.MemoryCompute.Score, r.CognitiveComplex.Score)
	}
}

func TestInsertStatement(t *testing.T) {
	// INSERT should parse without error and have zero score
	r, err := ScoreQuery("INSERT INTO users (name) VALUES ('test')")
	if err != nil {
		t.Fatal(err)
	}
	if r.TotalScore != 0 {
		t.Errorf("INSERT: got score %d, want 0", r.TotalScore)
	}
}

func TestDeleteWithWhere(t *testing.T) {
	r, err := ScoreQuery("DELETE FROM users WHERE id = 1")
	if err != nil {
		t.Fatal(err)
	}
	assertNoFinding(t, r.Efficiency.Findings, "missing-where-clause")
}

func TestNestedParenBoolean(t *testing.T) {
	r, err := ScoreQuery("SELECT id FROM users WHERE (id = 1 OR id = 2) AND (status = 'active' OR (role = 'admin' AND verified = 1))")
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, r.CognitiveComplex.Findings, "boolean-nesting")
}

func TestNotExpression(t *testing.T) {
	r, err := ScoreQuery("SELECT id FROM users WHERE NOT (status = 'inactive')")
	if err != nil {
		t.Fatal(err)
	}
	// NOT doesn't trigger boolean-nesting (depth 0), but should parse fine
	if r.Dialect != "mysql" {
		t.Errorf("dialect = %q", r.Dialect)
	}
}

func TestComparisonInWhere(t *testing.T) {
	r, err := ScoreQuery("SELECT id FROM users WHERE age > 18 AND name LIKE 'A%'")
	if err != nil {
		t.Fatal(err)
	}
	// LIKE without leading wildcard should not trigger like-leading-wildcard
	assertNoFinding(t, r.Efficiency.Findings, "like-leading-wildcard")
}

// --- helpers ---

func assertFinding(t *testing.T, findings []dialect.Finding, rule string) {
	t.Helper()
	for _, f := range findings {
		if f.Rule == rule {
			return
		}
	}
	t.Errorf("expected finding %q, got: %v", rule, findingRules(findings))
}

func assertNoFinding(t *testing.T, findings []dialect.Finding, rule string) {
	t.Helper()
	for _, f := range findings {
		if f.Rule == rule {
			t.Errorf("unexpected finding %q", rule)
			return
		}
	}
}

func findingRules(findings []dialect.Finding) []string {
	var rules []string
	for _, f := range findings {
		rules = append(rules, f.Rule)
	}
	return rules
}
