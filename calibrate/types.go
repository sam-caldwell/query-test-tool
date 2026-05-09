// Package calibrate implements a weight calibration system that generates schemas,
// queries, runs EXPLAIN ANALYZE against PostgreSQL, and uses linear regression
// to determine optimal scoring weights for sqlscore.
package calibrate

import (
	"encoding/json"
	"runtime"
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
	SchemaCount    int // target number of schemas (default 5000)
	QueryCount     int // target number of queries (default 1000000)
	RowsPerTable   int // base rows per table (default 70000; tiered multipliers apply)
	Workers        int // concurrency for EXPLAIN execution
	BatchSize      int // schemas per batch in batch-and-drop mode (default 10)
	StatementTimeout int // per-query timeout in ms
	DatabaseName   string // database to use
	SchemaFile     string // optional .SQL file for custom schema import
}

// DefaultConfig returns a PipelineConfig with hardware-aware defaults.
// Workers are set to 3× NumCPU, which balances I/O-bound work against
// system stability. The calibrate binary will also tune PostgreSQL's
// max_connections to match at startup.
func DefaultConfig() PipelineConfig {
	workers := runtime.NumCPU() * 3
	if workers < 4 {
		workers = 4
	}
	return PipelineConfig{
		DSN:              "postgres://localhost:5432/sqlscore_calibrate?sslmode=disable",
		SchemaCount:      5000,
		QueryCount:       1000000,
		RowsPerTable:     70000,
		BatchSize:        10,
		Workers:          workers,
		StatementTimeout: 5000,
		DatabaseName:     "sqlscore_calibrate",
	}
}
