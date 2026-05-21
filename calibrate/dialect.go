package calibrate

import (
	"context"
	"database/sql"
)

// DialectDB abstracts the database layer for calibration, allowing both
// PostgreSQL and MySQL implementations to share the same pipeline logic.
type DialectDB interface {
	Close() error
	Conn() *sql.DB

	// Tracking tables
	InitTrackingTables(ctx context.Context) error
	CreateResultPartitions(ctx context.Context, totalBatches int) error

	// Families
	InsertFamily(ctx context.Context, domain, name, description string) (int, error)
	GetExistingFamilies(ctx context.Context) (map[string]int, error)

	// Schema instances
	InsertSchemaInstance(ctx context.Context, familyID int, schemaName string, isOptimal bool, mutations []string, ddl string) (int, error)
	CountSchemaInstancesForFamily(ctx context.Context, familyID int) (int, error)
	GetSchemasWithoutResults(ctx context.Context) ([]string, error)
	GetPendingSchemasWithFamily(ctx context.Context) ([]PendingSchema, error)
	GetFamilySchemas(ctx context.Context, familyID int) ([]SchemaInstance, error)

	// Schema lifecycle
	ApplySchema(ctx context.Context, schemaName, ddl string) error
	ApplyIndexesAndFKs(ctx context.Context, ddl string) error
	DropOrphanSchemas(ctx context.Context) (int, error)

	// Queries
	InsertQueryBatch(ctx context.Context, queries []GeneratedQuery) error
	CountQueriesForFamily(ctx context.Context, familyID int) (int, error)
	GetQueriesForFamily(ctx context.Context, familyID int) ([]GeneratedQuery, error)

	// EXPLAIN
	RunExplain(ctx context.Context, schemaName, querySQL string) (*ExplainResult, error)

	// Results
	InsertResult(ctx context.Context, r *ScoredResult, batchID int) error
	CountTotalResults(ctx context.Context) (int, error)
	LoadResultsForRegression(ctx context.Context) ([]RegressionRow, error)
}

// DDLGenerator generates dialect-specific DDL for schema creation.
type DDLGenerator interface {
	GenerateDDL(d Domain, schemaName string) string
	GenerateDDLTablesOnly(d Domain, schemaName string) string
	GenerateDDLIndexesAndFKs(d Domain, schemaName string) string
}

// DataPopulator generates and executes data population SQL.
type DataPopulator interface {
	PopulateSchema(ctx context.Context, schemaName string, domain Domain) error
}

// DialectKit bundles all dialect-specific components needed by the pipeline.
type DialectKit struct {
	// NewDB creates a new database connection for this dialect.
	NewDB func(cfg PipelineConfig) (DialectDB, error)

	// DDL generates dialect-specific DDL.
	DDL DDLGenerator

	// NewDataPopulator creates a data populator for this dialect.
	NewDataPopulator func(db DialectDB, cfg PipelineConfig) DataPopulator

	// MapTypes converts archetype column types to dialect-specific types.
	// For PostgreSQL this is identity; for MySQL it converts SERIAL->AUTO_INCREMENT, etc.
	MapTypes func(d Domain) Domain

	// ScorerDialect is the dialect name to pass to scorer.ScoreQueryWithDialect.
	ScorerDialect string
}
