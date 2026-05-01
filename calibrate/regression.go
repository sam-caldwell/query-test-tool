package calibrate

import (
	"fmt"
	"math"
	"time"
)

// Feature names for the regression — one per sqlscore rule.
var RuleFeatures = []string{
	"select-star",
	"missing-predicate",
	"correlated-subquery",
	"non-sargable",
	"distinct-dedup",
	"unbounded-sort",
	"group-by-fanout",
	"window-function",
	"cartesian-product",
	"subquery-nesting",
	"join",
	"boolean-nesting",
	"cte",
	"case-expression",
	"set-operation",
}

// CalculateWeights performs OLS regression on the calibration results.
// The model: cost_ratio = β₀ + Σ βᵢ × feature_i
// Where feature_i is the count of times rule i appears in findings.
func CalculateWeights(rows []RegressionRow) (*CalibratedWeights, error) {
	if len(rows) < len(RuleFeatures)+1 {
		return nil, fmt.Errorf("insufficient data: have %d rows, need at least %d",
			len(rows), len(RuleFeatures)+1)
	}

	n := len(rows)
	p := len(RuleFeatures) + 1 // +1 for intercept

	// Build X matrix and y vector
	// X[i] = [1, count_of_rule_0, count_of_rule_1, ...]
	// y[i] = cost_ratio
	X := make([][]float64, n)
	y := make([]float64, n)

	for i, row := range rows {
		X[i] = make([]float64, p)
		X[i][0] = 1.0 // intercept

		// Count findings per rule
		findingCounts := make(map[string]int)
		for _, f := range row.Findings {
			findingCounts[f]++
		}

		for j, rule := range RuleFeatures {
			X[i][j+1] = float64(findingCounts[rule])
		}

		// Use log(cost_ratio) for better regression fit on multiplicative costs
		if row.CostRatio > 0 {
			y[i] = math.Log(row.CostRatio)
		}
	}

	// Compute X^T X (p×p matrix) with ridge regularization (λ = 0.01)
	// Ridge prevents singularity when features are sparse/collinear.
	lambda := 0.01
	XtX := make([][]float64, p)
	for i := range XtX {
		XtX[i] = make([]float64, p)
	}
	for i := 0; i < p; i++ {
		for j := 0; j < p; j++ {
			sum := 0.0
			for k := 0; k < n; k++ {
				sum += X[k][i] * X[k][j]
			}
			XtX[i][j] = sum
		}
		// Add λ·n to diagonal (skip intercept at index 0)
		if i > 0 {
			XtX[i][i] += lambda * float64(n)
		}
	}

	// Compute X^T y (p×1 vector)
	Xty := make([]float64, p)
	for i := 0; i < p; i++ {
		sum := 0.0
		for k := 0; k < n; k++ {
			sum += X[k][i] * y[k]
		}
		Xty[i] = sum
	}

	// Solve (X^T X) β = X^T y via Gauss-Jordan elimination
	beta, err := solveLinearSystem(XtX, Xty)
	if err != nil {
		return nil, fmt.Errorf("regression solve failed: %w", err)
	}

	// Compute R² (coefficient of determination)
	yMean := 0.0
	for _, yi := range y {
		yMean += yi
	}
	yMean /= float64(n)

	ssRes := 0.0
	ssTot := 0.0
	for i := 0; i < n; i++ {
		predicted := 0.0
		for j := 0; j < p; j++ {
			predicted += X[i][j] * beta[j]
		}
		ssRes += (y[i] - predicted) * (y[i] - predicted)
		ssTot += (y[i] - yMean) * (y[i] - yMean)
	}

	rSquared := 0.0
	if ssTot > 0 {
		rSquared = 1.0 - ssRes/ssTot
	}

	// Build weight map — exponentiate since we used log(cost_ratio)
	// The weight represents the multiplicative cost factor per occurrence of the rule.
	// We convert to an additive score by using the raw beta (log-scale coefficient).
	weights := make(map[string]float64)
	weights["intercept"] = beta[0]
	for i, rule := range RuleFeatures {
		// Convert log-scale coefficient to a practical penalty weight.
		// Scale: beta of 0.5 in log space ≈ 1.65× cost increase per occurrence.
		// We normalize to a 1-20 range for user-friendly scoring.
		w := beta[i+1] * 10.0 // scale factor
		if w < 0 {
			w = 0 // negative weights don't make sense for penalties
		}
		weights[rule] = math.Round(w*100) / 100
	}

	return &CalibratedWeights{
		Weights:     weights,
		RSquared:    rSquared,
		SampleSize:  n,
		GeneratedAt: time.Now(),
	}, nil
}

// solveLinearSystem solves Ax = b using Gauss-Jordan elimination with partial pivoting.
func solveLinearSystem(A [][]float64, b []float64) ([]float64, error) {
	n := len(b)

	// Create augmented matrix [A|b]
	aug := make([][]float64, n)
	for i := range aug {
		aug[i] = make([]float64, n+1)
		copy(aug[i], A[i])
		aug[i][n] = b[i]
	}

	// Forward elimination with partial pivoting
	for col := 0; col < n; col++ {
		// Find pivot
		maxRow := col
		maxVal := math.Abs(aug[col][col])
		for row := col + 1; row < n; row++ {
			if math.Abs(aug[row][col]) > maxVal {
				maxVal = math.Abs(aug[row][col])
				maxRow = row
			}
		}

		if maxVal < 1e-12 {
			return nil, fmt.Errorf("matrix is singular or near-singular at column %d", col)
		}

		// Swap rows
		aug[col], aug[maxRow] = aug[maxRow], aug[col]

		// Eliminate below
		for row := col + 1; row < n; row++ {
			factor := aug[row][col] / aug[col][col]
			for j := col; j <= n; j++ {
				aug[row][j] -= factor * aug[col][j]
			}
		}
	}

	// Back substitution
	x := make([]float64, n)
	for i := n - 1; i >= 0; i-- {
		x[i] = aug[i][n]
		for j := i + 1; j < n; j++ {
			x[i] -= aug[i][j] * x[j]
		}
		x[i] /= aug[i][i]
	}

	return x, nil
}

// ValidateWeights checks that calibrated weights are reasonable.
func ValidateWeights(cw *CalibratedWeights) []string {
	var issues []string

	if cw.RSquared < 0.1 {
		issues = append(issues, fmt.Sprintf("Low R²: %.3f (model explains < 10%% of variance)", cw.RSquared))
	}

	zeroCount := 0
	for rule, w := range cw.Weights {
		if rule == "intercept" {
			continue
		}
		if w == 0 {
			zeroCount++
		}
		if w > 50 {
			issues = append(issues, fmt.Sprintf("Unusually high weight for %s: %.2f", rule, w))
		}
	}

	if zeroCount > len(RuleFeatures)/2 {
		issues = append(issues, fmt.Sprintf("%d/%d rules have zero weight — insufficient signal", zeroCount, len(RuleFeatures)))
	}

	return issues
}
