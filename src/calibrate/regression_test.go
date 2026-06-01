package calibrate

import (
	"math"
	"testing"
)

func TestSolveLinearSystem_Simple(t *testing.T) {
	// 2x + y = 5
	// x + 3y = 10
	// Solution: x = 1, y = 3
	A := [][]float64{
		{2, 1},
		{1, 3},
	}
	b := []float64{5, 10}

	x, err := solveLinearSystem(A, b)
	if err != nil {
		t.Fatal(err)
	}

	if math.Abs(x[0]-1.0) > 1e-10 {
		t.Errorf("x[0] = %f, want 1.0", x[0])
	}
	if math.Abs(x[1]-3.0) > 1e-10 {
		t.Errorf("x[1] = %f, want 3.0", x[1])
	}
}

func TestSolveLinearSystem_Identity(t *testing.T) {
	A := [][]float64{
		{1, 0, 0},
		{0, 1, 0},
		{0, 0, 1},
	}
	b := []float64{7, 11, 13}

	x, err := solveLinearSystem(A, b)
	if err != nil {
		t.Fatal(err)
	}

	for i, want := range b {
		if math.Abs(x[i]-want) > 1e-10 {
			t.Errorf("x[%d] = %f, want %f", i, x[i], want)
		}
	}
}

func TestSolveLinearSystem_Singular(t *testing.T) {
	A := [][]float64{
		{1, 2},
		{2, 4}, // linearly dependent
	}
	b := []float64{3, 6}

	_, err := solveLinearSystem(A, b)
	if err == nil {
		t.Error("expected error for singular matrix")
	}
}

func TestSolveLinearSystem_LargerSystem(t *testing.T) {
	// 3x3 system with known solution
	A := [][]float64{
		{3, 2, -1},
		{2, -2, 4},
		{-1, 0.5, -1},
	}
	b := []float64{1, -2, 0}

	x, err := solveLinearSystem(A, b)
	if err != nil {
		t.Fatal(err)
	}

	// Verify Ax = b
	for i := 0; i < 3; i++ {
		sum := 0.0
		for j := 0; j < 3; j++ {
			sum += A[i][j] * x[j]
		}
		if math.Abs(sum-b[i]) > 1e-8 {
			t.Errorf("row %d: Ax = %f, want %f", i, sum, b[i])
		}
	}
}

func TestCalculateWeights_Basic(t *testing.T) {
	// Create synthetic data where rule "select-star" adds 2x cost
	// and "unbounded-sort" adds 1.5x cost
	rows := make([]RegressionRow, 200)
	for i := range rows {
		hasStar := i%3 == 0
		hasSort := i%2 == 0

		costRatio := 1.0
		var findings []string
		if hasStar {
			findings = append(findings, "select-star")
			costRatio *= 2.0
		}
		if hasSort {
			findings = append(findings, "unbounded-sort")
			costRatio *= 1.5
		}
		// Add some noise
		costRatio *= 1.0 + float64(i%5)*0.01

		rows[i] = RegressionRow{
			QueryID:    i,
			CostRatio:  costRatio,
			TimeRatio:  costRatio,
			Findings:   findings,
			Mutations:  nil,
		}
	}

	weights, err := CalculateWeights(rows)
	if err != nil {
		t.Fatal(err)
	}

	// R² should be high since we constructed the data
	if weights.RSquared < 0.8 {
		t.Errorf("R² = %f, expected > 0.8 for synthetic data", weights.RSquared)
	}

	// select-star weight should be positive and significant
	if weights.Weights["select-star"] <= 0 {
		t.Errorf("select-star weight = %f, expected positive", weights.Weights["select-star"])
	}

	// unbounded-sort weight should be positive
	if weights.Weights["unbounded-sort"] <= 0 {
		t.Errorf("unbounded-sort weight = %f, expected positive", weights.Weights["unbounded-sort"])
	}

	// Rules that aren't present should have near-zero weight
	if weights.Weights["cartesian-product"] > 1.0 {
		t.Errorf("cartesian-product weight = %f, expected near 0", weights.Weights["cartesian-product"])
	}
}

func TestCalculateWeights_InsufficientData(t *testing.T) {
	rows := []RegressionRow{{QueryID: 1, CostRatio: 1.5, Findings: []string{"select-star"}}}
	_, err := CalculateWeights(rows)
	if err == nil {
		t.Error("expected error for insufficient data")
	}
}

func TestValidateWeights(t *testing.T) {
	// Good weights
	good := &CalibratedWeights{
		RSquared: 0.75,
		Weights: map[string]float64{
			"select-star":        5.0,
			"unbounded-sort":     8.0,
			"non-sargable":       10.0,
			"missing-predicate":  12.0,
			"correlated-subquery": 15.0,
			"distinct-dedup":     6.0,
			"group-by-fanout":    4.0,
			"window-function":    7.0,
			"cartesian-product":  15.0,
			"subquery-nesting":   3.0,
			"join":               2.0,
			"outer-join":         3.0,
			"boolean-nesting":    2.0,
			"cte":                2.0,
			"case-expression":    1.0,
			"set-operation":      3.0,
			"join-count-squared":         1.0,
			"null-coalesce-in-predicate": 2.0,
			"null-check-chain":           2.0,
			"expensive-function":         2.0,
			"volatile-function":          3.0,
			"missing-where-clause":       15.0,
			"large-offset":               8.0,
			"recursive-cte":              10.0,
			"large-in-list":              5.0,
			"like-leading-wildcard":      10.0,
			"implicit-cast-in-predicate": 10.0,
			"lateral-join":               8.0,
			"returning-clause":           2.0,
			"grouping-sets":              8.0,
			"for-update-lock":            5.0,
			"union-distinct":             5.0,
			"ddl-statement":              5.0,
			"cascade-drop":               15.0,
		},
	}
	issues := ValidateWeights(good)
	if len(issues) != 0 {
		t.Errorf("good weights should have no issues, got: %v", issues)
	}

	// Low R²
	bad := &CalibratedWeights{RSquared: 0.05, Weights: good.Weights}
	issues = ValidateWeights(bad)
	if len(issues) == 0 {
		t.Error("expected warning for low R��")
	}

	// Too many zeros
	zeros := &CalibratedWeights{
		RSquared: 0.5,
		Weights:  map[string]float64{"intercept": 1.0},
	}
	for _, r := range RuleFeatures {
		zeros.Weights[r] = 0
	}
	issues = ValidateWeights(zeros)
	if len(issues) == 0 {
		t.Error("expected warning for many zero weights")
	}

	// Unusually high weight
	high := &CalibratedWeights{RSquared: 0.5, Weights: map[string]float64{"select-star": 100.0}}
	issues = ValidateWeights(high)
	if len(issues) == 0 {
		t.Error("expected warning for high weight")
	}
}

func TestRuleFeatures(t *testing.T) {
	if len(RuleFeatures) != 35 {
		t.Errorf("expected 35 rule features, got %d", len(RuleFeatures))
	}

	seen := make(map[string]bool)
	for _, r := range RuleFeatures {
		if r == "" {
			t.Error("empty rule feature")
		}
		if seen[r] {
			t.Errorf("duplicate rule feature: %s", r)
		}
		seen[r] = true
	}
}
