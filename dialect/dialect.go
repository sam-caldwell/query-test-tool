// Package dialect provides the abstraction layer for multi-database support.
// Each supported database dialect registers a parser and weights file.
// Adding a new dialect requires: a weights JSON file, a parser implementation,
// and a registration call.
package dialect

import "fmt"

// Dialect identifies a supported database type.
type Dialect string

const (
	PostgreSQL Dialect = "postgresql"
	MySQL      Dialect = "mysql"
)

// Valid returns true if the dialect is a known, registered type.
func (d Dialect) Valid() bool {
	_, ok := registry[d]
	return ok
}

// String returns the dialect name.
func (d Dialect) String() string { return string(d) }

// Registration bundles a parser and weight data for a dialect.
type Registration struct {
	Name        Dialect
	Description string
	WeightsData []byte // raw JSON bytes (embedded at build time)
}

// Finding represents a single issue detected in a query.
type Finding struct {
	Rule        string `json:"rule"`
	Description string `json:"description"`
	Penalty     int    `json:"penalty"`
	Category    string `json:"category"`
}

// DimensionScore holds the score for a single dimension.
type DimensionScore struct {
	Name     string    `json:"name"`
	Score    int       `json:"score"`
	Findings []Finding `json:"findings"`
}

// Report is the complete scoring result for a query.
type Report struct {
	SQL              string         `json:"sql"`
	Dialect          string         `json:"dialect"`
	TotalScore       int            `json:"total_score"`
	Efficiency       DimensionScore `json:"efficiency"`
	MemoryCompute    DimensionScore `json:"memory_compute"`
	CognitiveComplex DimensionScore `json:"cognitive_complexity"`
}

// WeightFunc is a function that returns the penalty weight for a rule.
// Set by the scorer package at init time to avoid import cycles.
var WeightFunc func(rule string) int

// Weight returns the penalty weight for a rule using the active dialect.
// Delegates to the scorer package's Weight function.
func Weight(rule string) int {
	if WeightFunc != nil {
		return WeightFunc(rule)
	}
	return 0
}

var registry = map[Dialect]*Registration{}

// Register adds a dialect to the global registry.
func Register(r *Registration) {
	registry[r.Name] = r
}

// Get returns the registration for a dialect, or an error if unsupported.
func Get(d Dialect) (*Registration, error) {
	r, ok := registry[d]
	if !ok {
		return nil, fmt.Errorf("unsupported dialect: %q (supported: %v)", d, Supported())
	}
	return r, nil
}

// Supported returns all registered dialect names.
func Supported() []Dialect {
	out := make([]Dialect, 0, len(registry))
	for d := range registry {
		out = append(out, d)
	}
	return out
}
