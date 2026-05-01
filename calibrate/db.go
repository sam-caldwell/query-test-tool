package calibrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	_ "github.com/lib/pq"
)

// DB wraps a PostgreSQL connection with calibration-specific operations.
type DB struct {
	conn *sql.DB
	cfg  PipelineConfig
}

// NewDB creates a new calibration database connection.
func NewDB(cfg PipelineConfig) (*DB, error) {
	conn, err := sql.Open("postgres", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	conn.SetMaxOpenConns(cfg.Workers + 2)
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	return &DB{conn: conn, cfg: cfg}, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// InitTrackingTables creates the calibration tracking schema and tables.
func (db *DB) InitTrackingTables(ctx context.Context) error {
	ddl := `
		CREATE SCHEMA IF NOT EXISTS calibration;

		CREATE TABLE IF NOT EXISTS calibration.families (
			id SERIAL PRIMARY KEY,
			domain TEXT NOT NULL,
			name TEXT NOT NULL UNIQUE,
			description TEXT,
			created_at TIMESTAMPTZ DEFAULT now()
		);

		CREATE TABLE IF NOT EXISTS calibration.schema_instances (
			id SERIAL PRIMARY KEY,
			family_id INT NOT NULL REFERENCES calibration.families(id),
			schema_name TEXT NOT NULL UNIQUE,
			is_optimal BOOLEAN NOT NULL DEFAULT false,
			mutations TEXT[] NOT NULL DEFAULT '{}',
			ddl TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT now()
		);

		CREATE TABLE IF NOT EXISTS calibration.queries (
			id SERIAL PRIMARY KEY,
			family_id INT NOT NULL REFERENCES calibration.families(id),
			sql_text TEXT NOT NULL,
			query_type TEXT NOT NULL,
			target_rules TEXT[] NOT NULL DEFAULT '{}',
			created_at TIMESTAMPTZ DEFAULT now()
		);

		CREATE TABLE IF NOT EXISTS calibration.results (
			id SERIAL PRIMARY KEY,
			query_id INT NOT NULL REFERENCES calibration.queries(id),
			schema_instance_id INT NOT NULL REFERENCES calibration.schema_instances(id),
			plan JSONB,
			total_cost DOUBLE PRECISION,
			startup_cost DOUBLE PRECISION,
			actual_time_ms DOUBLE PRECISION,
			rows_planned BIGINT,
			rows_actual BIGINT,
			shared_hit_blocks BIGINT,
			shared_read_blocks BIGINT,
			score_total INT,
			score_efficiency INT,
			score_memory_compute INT,
			score_cognitive INT,
			findings TEXT[] DEFAULT '{}',
			created_at TIMESTAMPTZ DEFAULT now()
		);

		CREATE INDEX IF NOT EXISTS idx_results_query_id ON calibration.results(query_id);
		CREATE INDEX IF NOT EXISTS idx_results_schema_id ON calibration.results(schema_instance_id);
		CREATE INDEX IF NOT EXISTS idx_schema_instances_family ON calibration.schema_instances(family_id);
		CREATE INDEX IF NOT EXISTS idx_queries_family ON calibration.queries(family_id);
	`
	_, err := db.conn.ExecContext(ctx, ddl)
	return err
}

// InsertFamily inserts a schema family and returns its ID.
func (db *DB) InsertFamily(ctx context.Context, domain, name, description string) (int, error) {
	var id int
	err := db.conn.QueryRowContext(ctx,
		`INSERT INTO calibration.families (domain, name, description) VALUES ($1, $2, $3)
		 ON CONFLICT (name) DO UPDATE SET description = EXCLUDED.description
		 RETURNING id`,
		domain, name, description,
	).Scan(&id)
	return id, err
}

// InsertSchemaInstance inserts a schema instance and returns its ID.
func (db *DB) InsertSchemaInstance(ctx context.Context, familyID int, schemaName string, isOptimal bool, mutations []string, ddl string) (int, error) {
	var id int
	mutArr := "{" + strings.Join(mutations, ",") + "}"
	err := db.conn.QueryRowContext(ctx,
		`INSERT INTO calibration.schema_instances (family_id, schema_name, is_optimal, mutations, ddl)
		 VALUES ($1, $2, $3, $4::text[], $5)
		 ON CONFLICT (schema_name) DO UPDATE SET ddl = EXCLUDED.ddl
		 RETURNING id`,
		familyID, schemaName, isOptimal, mutArr, ddl,
	).Scan(&id)
	return id, err
}

// ApplySchema executes DDL to create a schema in PostgreSQL.
func (db *DB) ApplySchema(ctx context.Context, schemaName, ddl string) error {
	// Drop and recreate
	_, err := db.conn.ExecContext(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
	if err != nil {
		return fmt.Errorf("dropping schema %s: %w", schemaName, err)
	}
	_, err = db.conn.ExecContext(ctx, ddl)
	if err != nil {
		return fmt.Errorf("applying schema %s: %w", schemaName, err)
	}
	return nil
}

// InsertQuery inserts a generated query and returns its ID.
func (db *DB) InsertQuery(ctx context.Context, familyID int, sqlText, queryType string, targetRules []string) (int, error) {
	var id int
	rulesArr := "{" + strings.Join(targetRules, ",") + "}"
	err := db.conn.QueryRowContext(ctx,
		`INSERT INTO calibration.queries (family_id, sql_text, query_type, target_rules)
		 VALUES ($1, $2, $3, $4::text[]) RETURNING id`,
		familyID, sqlText, queryType, rulesArr,
	).Scan(&id)
	return id, err
}

// InsertQueryBatch inserts multiple queries efficiently.
func (db *DB) InsertQueryBatch(ctx context.Context, queries []GeneratedQuery) error {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO calibration.queries (family_id, sql_text, query_type, target_rules) VALUES ($1, $2, $3, $4::text[])`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, q := range queries {
		rulesArr := "{" + strings.Join(q.TargetRules, ",") + "}"
		if _, err := stmt.ExecContext(ctx, q.FamilyID, q.SQL, q.QueryType, rulesArr); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RunExplain executes EXPLAIN (ANALYZE, FORMAT JSON) for a query within a schema.
func (db *DB) RunExplain(ctx context.Context, schemaName, querySQL string) (*ExplainResult, error) {
	// Set search path and statement timeout
	setPath := fmt.Sprintf("SET search_path TO %s, public; SET statement_timeout = '%dms';",
		schemaName, db.cfg.StatementTimeout)

	explainSQL := setPath + " EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) " + querySQL

	var planJSON string
	err := db.conn.QueryRowContext(ctx, explainSQL).Scan(&planJSON)
	if err != nil {
		return nil, fmt.Errorf("EXPLAIN failed: %w", err)
	}

	return parseExplainJSON([]byte(planJSON))
}

// InsertResult stores an EXPLAIN result.
func (db *DB) InsertResult(ctx context.Context, r *ScoredResult) error {
	findingsArr := "{" + strings.Join(r.Findings, ",") + "}"
	_, err := db.conn.ExecContext(ctx,
		`INSERT INTO calibration.results
		 (query_id, schema_instance_id, plan, total_cost, startup_cost, actual_time_ms,
		  rows_planned, rows_actual, shared_hit_blocks, shared_read_blocks,
		  score_total, score_efficiency, score_memory_compute, score_cognitive, findings)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::text[])`,
		r.QueryID, r.SchemaInstanceID, r.Plan, r.TotalCost, r.StartupCost, r.ActualTimeMs,
		r.RowsPlanned, r.RowsActual, r.SharedHitBlocks, r.SharedReadBlocks,
		r.ScoreTotal, r.ScoreEfficiency, r.ScoreMemory, r.ScoreCognitive, findingsArr,
	)
	return err
}

// LoadResultsForRegression loads all results paired with their optimal counterpart.
func (db *DB) LoadResultsForRegression(ctx context.Context) ([]RegressionRow, error) {
	query := `
		WITH optimal_costs AS (
			SELECT q.id AS query_id, q.family_id, r.total_cost, r.actual_time_ms
			FROM calibration.results r
			JOIN calibration.queries q ON r.query_id = q.id
			JOIN calibration.schema_instances si ON r.schema_instance_id = si.id
			WHERE si.is_optimal = true AND r.total_cost > 0
		),
		degraded AS (
			SELECT q.id AS query_id, q.family_id, r.total_cost, r.actual_time_ms,
				   si.mutations, r.findings,
				   r.score_total, r.score_efficiency, r.score_memory_compute, r.score_cognitive
			FROM calibration.results r
			JOIN calibration.queries q ON r.query_id = q.id
			JOIN calibration.schema_instances si ON r.schema_instance_id = si.id
			WHERE si.is_optimal = false AND r.total_cost > 0
		)
		SELECT d.query_id, d.mutations, d.findings,
		       d.total_cost / NULLIF(o.total_cost, 0) AS cost_ratio,
		       d.actual_time_ms / NULLIF(o.actual_time_ms, 0) AS time_ratio,
		       d.score_total, d.score_efficiency, d.score_memory_compute, d.score_cognitive
		FROM degraded d
		JOIN optimal_costs o ON d.query_id = o.query_id AND d.family_id = o.family_id
		WHERE d.total_cost / NULLIF(o.total_cost, 0) IS NOT NULL
	`

	rows, err := db.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []RegressionRow
	for rows.Next() {
		var rr RegressionRow
		var mutations, findings string
		err := rows.Scan(&rr.QueryID, &mutations, &findings,
			&rr.CostRatio, &rr.TimeRatio,
			&rr.ScoreTotal, &rr.ScoreEfficiency, &rr.ScoreMemory, &rr.ScoreCognitive)
		if err != nil {
			return nil, err
		}
		rr.Mutations = parseArray(mutations)
		rr.Findings = parseArray(findings)
		results = append(results, rr)
	}
	return results, rows.Err()
}

// GetFamilySchemas returns all schema instances for a family.
func (db *DB) GetFamilySchemas(ctx context.Context, familyID int) ([]SchemaInstance, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, family_id, schema_name, is_optimal, mutations, ddl
		 FROM calibration.schema_instances WHERE family_id = $1`, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []SchemaInstance
	for rows.Next() {
		var si SchemaInstance
		var mutations string
		if err := rows.Scan(&si.ID, &si.FamilyID, &si.SchemaName, &si.IsOptimal, &mutations, &si.DDL); err != nil {
			return nil, err
		}
		si.Mutations = parseArray(mutations)
		instances = append(instances, si)
	}
	return instances, rows.Err()
}

// GetQueriesForFamily returns all queries for a family.
func (db *DB) GetQueriesForFamily(ctx context.Context, familyID int) ([]GeneratedQuery, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, family_id, sql_text, query_type, target_rules
		 FROM calibration.queries WHERE family_id = $1`, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []GeneratedQuery
	for rows.Next() {
		var q GeneratedQuery
		var rules string
		if err := rows.Scan(&q.ID, &q.FamilyID, &q.SQL, &q.QueryType, &rules); err != nil {
			return nil, err
		}
		q.TargetRules = parseArray(rules)
		queries = append(queries, q)
	}
	return queries, rows.Err()
}

// RegressionRow is a single data point for the regression.
type RegressionRow struct {
	QueryID         int
	Mutations       []string
	Findings        []string
	CostRatio       float64
	TimeRatio       float64
	ScoreTotal      int
	ScoreEfficiency int
	ScoreMemory     int
	ScoreCognitive  int
}

// parseExplainJSON extracts key metrics from EXPLAIN JSON output.
func parseExplainJSON(data []byte) (*ExplainResult, error) {
	var plans []struct {
		Plan struct {
			TotalCost       float64 `json:"Total Cost"`
			StartupCost     float64 `json:"Startup Cost"`
			ActualTotalTime float64 `json:"Actual Total Time"`
			PlanRows        int64   `json:"Plan Rows"`
			ActualRows      int64   `json:"Actual Rows"`
			SharedHitBlocks int64   `json:"Shared Hit Blocks"`
			SharedReadBlocks int64  `json:"Shared Read Blocks"`
		} `json:"Plan"`
	}

	if err := json.Unmarshal(data, &plans); err != nil {
		return nil, fmt.Errorf("parsing EXPLAIN JSON: %w", err)
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("empty EXPLAIN output")
	}

	p := plans[0].Plan
	return &ExplainResult{
		Plan:             data,
		TotalCost:        p.TotalCost,
		StartupCost:      p.StartupCost,
		ActualTimeMs:     p.ActualTotalTime,
		RowsPlanned:      p.PlanRows,
		RowsActual:       p.ActualRows,
		SharedHitBlocks:  p.SharedHitBlocks,
		SharedReadBlocks: p.SharedReadBlocks,
	}, nil
}

// parseArray parses a PostgreSQL text array representation.
func parseArray(s string) []string {
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
