// Package calibrate implements a weight calibration system that generates schemas,
// queries, runs EXPLAIN ANALYZE against PostgreSQL, and uses linear regression
// to determine optimal scoring weights for sqlscore.
package calibrate

import (
	"encoding/json"
	"time"
)

// Domain represents a business domain archetype (e.g., e-commerce, blog).
type Domain struct {
	Name        string
	Description string
	Tables      []TableDef
	Indexes     []IndexDef
	ForeignKeys []FKDef
}

// TableDef defines a table's structure.
type TableDef struct {
	Name    string
	Columns []ColumnDef
}

// ColumnDef defines a column.
type ColumnDef struct {
	Name     string
	Type     string
	NotNull  bool
	Default  string
	IsSerial bool // SERIAL/BIGSERIAL primary key
}

// IndexDef defines an index.
type IndexDef struct {
	Name       string
	Table      string
	Columns    []string
	Unique     bool
	Expression string // for expression indexes, e.g., "LOWER(email)"
}

// FKDef defines a foreign key constraint.
type FKDef struct {
	Name       string
	Table      string
	Column     string
	RefTable   string
	RefColumn  string
}

// Mutation represents a schema degradation that introduces an antipattern.
type Mutation struct {
	Name        string   // unique identifier, e.g., "drop_idx_users_email"
	Description string
	Rules       []string // sqlscore rules this mutation exercises
	Apply       func(d *Domain) // modifies domain in place
}

// SchemaFamily tracks a group of related schema instances.
type SchemaFamily struct {
	ID          int
	Domain      string
	Name        string
	Description string
}

// SchemaInstance is a single schema variant within a family.
type SchemaInstance struct {
	ID         int
	FamilyID   int
	SchemaName string // PostgreSQL schema name
	IsOptimal  bool
	Mutations  []string // mutation names applied
	DDL        string   // full DDL to create the schema
}

// QueryTemplate defines how to generate queries for a specific antipattern.
type QueryTemplate struct {
	Name       string   // template identifier
	Rules      []string // sqlscore rules this query exercises
	QueryType  string   // 'select_star', 'join', 'subquery', etc.
	Generate   func(inst SchemaInstance, domain Domain, seed int64) string
}

// GeneratedQuery is a query ready for execution.
type GeneratedQuery struct {
	ID         int
	FamilyID   int
	SQL        string
	QueryType  string
	TargetRules []string
}

// ExplainResult holds the parsed output of EXPLAIN ANALYZE.
type ExplainResult struct {
	ID               int
	QueryID          int
	SchemaInstanceID int
	Plan             json.RawMessage
	TotalCost        float64
	StartupCost      float64
	ActualTimeMs     float64
	RowsPlanned      int64
	RowsActual       int64
	SharedHitBlocks  int64
	SharedReadBlocks int64
}

// ScoredResult combines EXPLAIN data with sqlscore output.
type ScoredResult struct {
	ExplainResult
	ScoreTotal      int
	ScoreEfficiency int
	ScoreMemory     int
	ScoreCognitive  int
	Findings        []string // rule names triggered
}

// CalibratedWeights is the output of the regression.
type CalibratedWeights struct {
	Weights     map[string]float64 `json:"weights"`
	RSquared    float64            `json:"r_squared"`
	SampleSize  int                `json:"sample_size"`
	GeneratedAt time.Time          `json:"generated_at"`
}

// PipelineConfig holds configuration for the calibration pipeline.
type PipelineConfig struct {
	DSN            string
	SchemaCount    int // target number of schemas (default 10000)
	QueryCount     int // target number of queries (default 1000000)
	RowsPerTable   int // rows to generate per table (default 1000)
	Workers        int // concurrency for EXPLAIN execution
	StatementTimeout int // per-query timeout in ms
	DatabaseName   string // database to use
}

// DefaultConfig returns a PipelineConfig with sensible defaults.
func DefaultConfig() PipelineConfig {
	return PipelineConfig{
		DSN:              "postgres://localhost:5432/sqlscore_calibrate?sslmode=disable",
		SchemaCount:      10000,
		QueryCount:       1000000,
		RowsPerTable:     1000,
		Workers:          8,
		StatementTimeout: 5000,
		DatabaseName:     "sqlscore_calibrate",
	}
}
