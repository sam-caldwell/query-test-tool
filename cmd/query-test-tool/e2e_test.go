package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// E2E tests that execute the built query-test-tool binary and verify that
// calibrated weights produce expected scores for known inputs.
// These tests confirm the embedded weights.json is loaded and applied correctly.

var e2eBinary string

func init() {
	// Build the binary once for e2e tests
	dir, err := os.MkdirTemp("", "query-test-tool-e2e")
	if err != nil {
		panic(err)
	}
	e2eBinary = filepath.Join(dir, "query-test-tool")
	cmd := exec.Command("go", "build", "-o", e2eBinary, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		panic("e2e build failed: " + string(out) + ": " + err.Error())
	}
}

// scoreReport is the JSON output structure from query-test-tool -format json.
type scoreReport struct {
	SQL              string         `json:"sql"`
	TotalScore       int            `json:"total_score"`
	Efficiency       dimensionScore `json:"efficiency"`
	MemoryCompute    dimensionScore `json:"memory_compute"`
	CognitiveComplex dimensionScore `json:"cognitive_complexity"`
}

type dimensionScore struct {
	Name     string    `json:"name"`
	Score    int       `json:"score"`
	Findings []finding `json:"findings"`
}

type finding struct {
	Rule    string `json:"rule"`
	Penalty int    `json:"penalty"`
}

func runScore(t *testing.T, sql string) scoreReport {
	t.Helper()
	cmd := exec.Command(e2eBinary, "-q", sql, "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("query-test-tool failed for %q: %v", sql, err)
	}
	var report scoreReport
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nraw: %s", err, out)
	}
	return report
}

func TestE2E_CleanQuery_ZeroScore(t *testing.T) {
	report := runScore(t, "SELECT id, name FROM users WHERE id = 1")
	if report.TotalScore != 0 {
		t.Errorf("clean query: got score %d, want 0", report.TotalScore)
	}
}

func TestE2E_SelectStar_Weight(t *testing.T) {
	report := runScore(t, "SELECT * FROM users WHERE id = 1")
	f := findFinding(report.Efficiency.Findings, "select-star")
	if f == nil {
		t.Fatal("expected select-star finding")
	}
	if f.Penalty < 1 {
		t.Errorf("select-star penalty: got %d, want >= 1", f.Penalty)
	}
	if report.TotalScore != f.Penalty {
		t.Errorf("total score %d should equal select-star penalty %d for this simple query", report.TotalScore, f.Penalty)
	}
}

func TestE2E_NonSargable_Weight(t *testing.T) {
	report := runScore(t, "SELECT id FROM users WHERE LOWER(email) = 'test@test.com'")
	f := findFinding(report.Efficiency.Findings, "non-sargable")
	if f == nil {
		t.Fatal("expected non-sargable finding")
	}
	// Calibrated weight for non-sargable should be significant (>= 10)
	if f.Penalty < 10 {
		t.Errorf("non-sargable penalty: got %d, want >= 10", f.Penalty)
	}
}

func TestE2E_UnboundedSort_Weight(t *testing.T) {
	report := runScore(t, "SELECT id FROM users ORDER BY name")
	f := findFinding(report.MemoryCompute.Findings, "unbounded-sort")
	if f == nil {
		t.Fatal("expected unbounded-sort finding")
	}
	// Calibrated weight for unbounded-sort should be significant (>= 10)
	if f.Penalty < 10 {
		t.Errorf("unbounded-sort penalty: got %d, want >= 10", f.Penalty)
	}
}

func TestE2E_BoundedSort_NoFinding(t *testing.T) {
	report := runScore(t, "SELECT id FROM users ORDER BY name LIMIT 10")
	f := findFinding(report.MemoryCompute.Findings, "unbounded-sort")
	if f != nil {
		t.Error("ORDER BY with LIMIT should not trigger unbounded-sort")
	}
}

func TestE2E_CorrelatedSubquery_Weight(t *testing.T) {
	report := runScore(t, "SELECT * FROM users WHERE EXISTS (SELECT 1 FROM orders WHERE orders.user_id = users.id)")
	f := findFinding(report.Efficiency.Findings, "correlated-subquery")
	if f == nil {
		t.Fatal("expected correlated-subquery finding")
	}
	// Calibrated weight should be high (>= 15)
	if f.Penalty < 15 {
		t.Errorf("correlated-subquery penalty: got %d, want >= 15", f.Penalty)
	}
}

func TestE2E_GroupByFanout_Weight(t *testing.T) {
	report := runScore(t, "SELECT dept, COUNT(*) FROM employees GROUP BY dept")
	f := findFinding(report.MemoryCompute.Findings, "group-by-fanout")
	if f == nil {
		t.Fatal("expected group-by-fanout finding")
	}
	if f.Penalty < 1 {
		t.Errorf("group-by-fanout penalty: got %d, want >= 1", f.Penalty)
	}
}

func TestE2E_CognitiveJoin_Weight(t *testing.T) {
	report := runScore(t, "SELECT u.id FROM users u JOIN orders o ON u.id = o.user_id JOIN items i ON o.id = i.order_id")
	joinCount := countFindings(report.CognitiveComplex.Findings, "join")
	if joinCount != 2 {
		t.Errorf("expected 2 join findings, got %d", joinCount)
	}
}

func TestE2E_SetOperation_Weight(t *testing.T) {
	report := runScore(t, "SELECT id FROM users UNION SELECT id FROM admins")
	f := findFinding(report.CognitiveComplex.Findings, "set-operation")
	if f == nil {
		t.Fatal("expected set-operation finding")
	}
	if f.Penalty < 1 {
		t.Errorf("set-operation penalty: got %d, want >= 1", f.Penalty)
	}
}

func TestE2E_ComplexQuery_MultipleFindings(t *testing.T) {
	sql := `SELECT DISTINCT * FROM users u
		JOIN orders o ON u.id = o.user_id
		WHERE LOWER(u.email) = 'test'
		ORDER BY o.created_at`
	report := runScore(t, sql)

	// Should have findings in all three dimensions
	if report.Efficiency.Score == 0 {
		t.Error("expected non-zero efficiency score")
	}
	if report.MemoryCompute.Score == 0 {
		t.Error("expected non-zero memory_compute score")
	}
	if report.CognitiveComplex.Score == 0 {
		t.Error("expected non-zero cognitive score")
	}

	// Total should be sum of dimensions
	expected := report.Efficiency.Score + report.MemoryCompute.Score + report.CognitiveComplex.Score
	if report.TotalScore != expected {
		t.Errorf("total %d != sum of dimensions %d", report.TotalScore, expected)
	}

	// Verify specific findings are present
	if findFinding(report.Efficiency.Findings, "select-star") == nil {
		t.Error("expected select-star finding")
	}
	if findFinding(report.Efficiency.Findings, "non-sargable") == nil {
		t.Error("expected non-sargable finding")
	}
	if findFinding(report.Efficiency.Findings, "distinct-dedup") == nil {
		t.Error("expected distinct-dedup finding")
	}
	if findFinding(report.MemoryCompute.Findings, "unbounded-sort") == nil {
		t.Error("expected unbounded-sort finding")
	}
	if findFinding(report.CognitiveComplex.Findings, "join") == nil {
		t.Error("expected join finding")
	}
}

func TestE2E_CartesianProduct_Weight(t *testing.T) {
	report := runScore(t, "SELECT * FROM users, orders")
	f := findFinding(report.MemoryCompute.Findings, "cartesian-product")
	if f == nil {
		t.Fatal("expected cartesian-product finding")
	}
	if f.Penalty < 1 {
		t.Errorf("cartesian-product penalty: got %d, want >= 1", f.Penalty)
	}
}

func TestE2E_WindowFunction_Weight(t *testing.T) {
	report := runScore(t, "SELECT id, row_number() OVER (ORDER BY id) FROM users")
	f := findFinding(report.MemoryCompute.Findings, "window-function")
	if f == nil {
		t.Fatal("expected window-function finding")
	}
	if f.Penalty < 1 {
		t.Errorf("window-function penalty: got %d, want >= 1", f.Penalty)
	}
}

func TestE2E_SubqueryNesting_DepthMultiplied(t *testing.T) {
	shallow := runScore(t, "SELECT * FROM users WHERE id IN (SELECT user_id FROM orders)")
	deep := runScore(t, "SELECT * FROM users WHERE id IN (SELECT user_id FROM orders WHERE order_id IN (SELECT id FROM items))")

	if deep.CognitiveComplex.Score <= shallow.CognitiveComplex.Score {
		t.Errorf("deeper nesting (%d) should score higher than shallow (%d)",
			deep.CognitiveComplex.Score, shallow.CognitiveComplex.Score)
	}
}

func TestE2E_InvalidSQL_ExitNonZero(t *testing.T) {
	cmd := exec.Command(e2eBinary, "-q", "NOT VALID SQL AT ALL")
	err := cmd.Run()
	if err == nil {
		t.Error("expected non-zero exit code for invalid SQL")
	}
}

func TestE2E_Version_ShowsCalibrated(t *testing.T) {
	cmd := exec.Command(e2eBinary, "--version")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	output := string(out)
	if !contains(output, "query-test-tool") {
		t.Errorf("version output should show query-test-tool, got: %s", output)
	}
}

func TestE2E_BooleanNesting_Weight(t *testing.T) {
	report := runScore(t, "SELECT * FROM users WHERE (status = 'active' AND verified = true) OR role = 'admin'")
	f := findFinding(report.CognitiveComplex.Findings, "boolean-nesting")
	if f == nil {
		t.Fatal("expected boolean-nesting finding")
	}
	if f.Penalty < 1 {
		t.Errorf("boolean-nesting penalty: got %d, want >= 1", f.Penalty)
	}
}

func TestE2E_CTE_Weight(t *testing.T) {
	report := runScore(t, "WITH cte AS (SELECT id FROM users) SELECT * FROM cte")
	f := findFinding(report.CognitiveComplex.Findings, "cte")
	if f == nil {
		t.Fatal("expected cte finding")
	}
	if f.Penalty < 1 {
		t.Errorf("cte penalty: got %d, want >= 1", f.Penalty)
	}
}

// --- helpers ---

func findFinding(findings []finding, rule string) *finding {
	for i := range findings {
		if findings[i].Rule == rule {
			return &findings[i]
		}
	}
	return nil
}

func countFindings(findings []finding, rule string) int {
	n := 0
	for _, f := range findings {
		if f.Rule == rule {
			n++
		}
	}
	return n
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
