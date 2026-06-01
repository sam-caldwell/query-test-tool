package calibrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// DB wraps a PostgreSQL connection with calibration-specific operations.
type DB struct {
	conn *sql.DB
	cfg  PipelineConfig
}

// NewDB creates a new calibration database connection. It auto-tunes
// PostgreSQL's max_connections to match the worker count if needed,
// and configures the Go connection pool to leave headroom for PG
// internal connections (autovacuum, stats, replication, etc.).
func NewDB(cfg PipelineConfig) (*DB, error) {
	// First, open a single connection to check and tune PG settings
	conn, err := sql.Open("postgres", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	// Check current max_connections and tune if needed.
	// This may restart PostgreSQL, killing our connection.
	if err := tunePGConnections(conn, cfg.Workers); err != nil {
		log.Printf("Warning: could not tune PostgreSQL connections: %v", err)
	}

	// Reconnect in case PG was restarted
	if err := conn.Ping(); err != nil {
		conn.Close()
		conn, err = sql.Open("postgres", cfg.DSN)
		if err != nil {
			return nil, fmt.Errorf("reconnecting after PG tune: %w", err)
		}
		// Wait for PG to be fully ready
		for i := 0; i < 10; i++ {
			if err := conn.Ping(); err == nil {
				break
			}
			time.Sleep(time.Second)
		}
		if err := conn.Ping(); err != nil {
			return nil, fmt.Errorf("PostgreSQL not ready after restart: %w", err)
		}
	}

	poolSize := cfg.Workers
	conn.SetMaxOpenConns(poolSize)
	conn.SetMaxIdleConns(poolSize)
	conn.SetConnMaxLifetime(30 * time.Minute)

	return &DB{conn: conn, cfg: cfg}, nil
}

// tunePGConnections checks PostgreSQL's max_connections and increases it
// if the current setting is too low for the requested worker count.
// Requires superuser privileges. If ALTER SYSTEM succeeds, it reloads
// the configuration (pg_reload_conf) to apply without a restart.
func tunePGConnections(conn *sql.DB, workers int) error {
	// We need workers + headroom for PG internal connections
	required := workers + 15 // 15 for autovacuum, stats, superuser slots

	var current int
	if err := conn.QueryRow("SHOW max_connections").Scan(&current); err != nil {
		return fmt.Errorf("querying max_connections: %w", err)
	}

	if current >= required {
		log.Printf("PostgreSQL max_connections=%d (sufficient for %d workers + 15 reserved)", current, workers)
		return nil
	}

	log.Printf("PostgreSQL max_connections=%d is too low for %d workers; increasing to %d", current, workers, required)

	// ALTER SYSTEM writes to postgresql.auto.conf
	_, err := conn.Exec(fmt.Sprintf("ALTER SYSTEM SET max_connections = %d", required))
	if err != nil {
		return fmt.Errorf("ALTER SYSTEM SET max_connections: %w (may need superuser)", err)
	}

	// max_connections requires a full restart, not just reload
	log.Printf("max_connections changed to %d — restarting PostgreSQL to apply...", required)

	if err := restartPostgreSQL(); err != nil {
		return fmt.Errorf("max_connections updated to %d in postgresql.auto.conf but restart failed: %w — restart PostgreSQL manually and re-run", required, err)
	}

	// Verify the new setting took effect
	var newMax int
	if err := conn.QueryRow("SHOW max_connections").Scan(&newMax); err != nil {
		// Connection may have been dropped by restart; this is expected
		log.Printf("Connection dropped during restart (expected) — will reconnect")
		return nil
	}
	if newMax >= required {
		log.Printf("PostgreSQL restarted successfully — max_connections now %d", newMax)
	}

	return nil
}

// restartPostgreSQL attempts to restart the PostgreSQL service.
// Tries common service management commands in order of preference.
func restartPostgreSQL() error {
	cmds := [][]string{
		{"sudo", "service", "postgresql", "restart"},
		{"sudo", "systemctl", "restart", "postgresql"},
		{"sudo", "pg_ctlcluster", "16", "main", "restart"},
		{"pg_ctl", "restart", "-D", "/var/lib/postgresql/16/main", "-m", "fast"},
	}

	for _, cmd := range cmds {
		c := exec.Command(cmd[0], cmd[1:]...)
		if output, err := c.CombinedOutput(); err == nil {
			log.Printf("PostgreSQL restarted via: %s", strings.Join(cmd, " "))
			// Wait for PG to be ready
			time.Sleep(2 * time.Second)
			return nil
		} else {
			log.Printf("Restart attempt %q failed: %v (%s)", strings.Join(cmd, " "), err, strings.TrimSpace(string(output)))
		}
	}

	return fmt.Errorf("all restart methods failed — restart PostgreSQL manually")
}

// Conn returns the underlying *sql.DB connection.
func (db *DB) Conn() *sql.DB {
	return db.conn
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
			id SERIAL,
			batch_id INT NOT NULL DEFAULT 0,
			query_id INT NOT NULL,
			schema_instance_id INT NOT NULL,
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
			created_at TIMESTAMPTZ DEFAULT now(),
			PRIMARY KEY (id, batch_id)
		) PARTITION BY LIST (batch_id);

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

// ApplyIndexesAndFKs executes DDL for indexes and foreign keys on an existing schema.
func (db *DB) ApplyIndexesAndFKs(ctx context.Context, ddl string) error {
	_, err := db.conn.ExecContext(ctx, ddl)
	return err
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

// CountQueriesForFamily returns how many queries exist for a given family.
func (db *DB) CountQueriesForFamily(ctx context.Context, familyID int) (int, error) {
	var count int
	err := db.conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM calibration.queries WHERE family_id = $1`, familyID).Scan(&count)
	return count, err
}

// SchemaHasResults returns true if a schema instance has any EXPLAIN results.
func (db *DB) SchemaHasResults(ctx context.Context, schemaName string) (bool, error) {
	var count int
	err := db.conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM calibration.results r
		 JOIN calibration.schema_instances si ON r.schema_instance_id = si.id
		 WHERE si.schema_name = $1`, schemaName).Scan(&count)
	return count > 0, err
}

// DropOrphanSchemas drops any data schemas (cal_*) that still exist in the database.
// Terminates autovacuum workers first to prevent deadlocks during DROP.
// Retries up to 3 times per schema and verifies all orphans are gone before returning.
func (db *DB) DropOrphanSchemas(ctx context.Context) (int, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT schema_name FROM information_schema.schemata
		 WHERE schema_name LIKE 'cal\_%' AND schema_name != 'calibration'`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var orphans []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return 0, err
		}
		orphans = append(orphans, name)
	}

	if len(orphans) == 0 {
		return 0, nil
	}

	dropped := 0
	for _, name := range orphans {
		var success bool
		for attempt := 0; attempt < 3; attempt++ {
			// Kill any autovacuum workers touching this schema before each attempt
			db.conn.ExecContext(ctx, fmt.Sprintf(
				`SELECT pg_terminate_backend(pid) FROM pg_stat_activity
				 WHERE query LIKE 'autovacuum%%' AND query LIKE '%%%s%%' AND pid != pg_backend_pid()`, name))

			_, err := db.conn.ExecContext(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", name))
			if err == nil {
				success = true
				break
			}
			// Brief pause before retry to let locks clear
			db.conn.ExecContext(ctx, "SELECT pg_sleep(0.5)")
		}
		if success {
			dropped++
		} else {
			log.Printf("Warning: failed to drop orphan schema %s after 3 attempts", name)
		}
	}

	// Verify: confirm no orphans remain
	var remaining int
	db.conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.schemata
		 WHERE schema_name LIKE 'cal\_%' AND schema_name != 'calibration'`).Scan(&remaining)
	if remaining > 0 {
		log.Printf("Warning: %d orphan schemas still remain after cleanup", remaining)
	}

	return dropped, nil
}

// CreateResultPartitions pre-creates partitions for all batches plus a legacy partition (batch 0).
func (db *DB) CreateResultPartitions(ctx context.Context, totalBatches int) error {
	for i := 0; i <= totalBatches; i++ {
		partName := fmt.Sprintf("calibration.results_b%04d", i)
		ddl := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF calibration.results FOR VALUES IN (%d)`,
			partName, i)
		if _, err := db.conn.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("creating partition %s: %w", partName, err)
		}
	}
	// Create indexes on each partition (done after creation for idempotency)
	for i := 0; i <= totalBatches; i++ {
		partName := fmt.Sprintf("calibration.results_b%04d", i)
		idxQuery := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_results_b%04d_query ON %s(query_id)`, i, partName)
		if _, err := db.conn.ExecContext(ctx, idxQuery); err != nil {
			return fmt.Errorf("creating index on %s: %w", partName, err)
		}
		idxSchema := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_results_b%04d_schema ON %s(schema_instance_id)`, i, partName)
		if _, err := db.conn.ExecContext(ctx, idxSchema); err != nil {
			return fmt.Errorf("creating index on %s: %w", partName, err)
		}
	}
	log.Printf("Created %d result partitions (b0000-b%04d)", totalBatches+1, totalBatches)
	return nil
}

// GetExistingFamilies returns all families already registered in the DB.
func (db *DB) GetExistingFamilies(ctx context.Context) (map[string]int, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT name, id FROM calibration.families`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var name string
		var id int
		if err := rows.Scan(&name, &id); err != nil {
			return nil, err
		}
		result[name] = id
	}
	return result, nil
}

// CountSchemaInstancesForFamily returns how many schema instances exist for a family.
func (db *DB) CountSchemaInstancesForFamily(ctx context.Context, familyID int) (int, error) {
	var count int
	err := db.conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM calibration.schema_instances WHERE family_id = $1`, familyID).Scan(&count)
	return count, err
}

// PendingSchema holds a schema name and its family info from the database.
type PendingSchema struct {
	SchemaName string
	FamilyID   int
	FamilyName string
	Domain     string
}

// GetSchemasWithoutResults returns schema names that have no EXPLAIN results yet.
func (db *DB) GetSchemasWithoutResults(ctx context.Context) ([]string, error) {
	pending, err := db.GetPendingSchemasWithFamily(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(pending))
	for i, p := range pending {
		names[i] = p.SchemaName
	}
	return names, nil
}

// GetPendingSchemasWithFamily returns schemas without results, including their
// actual family assignment from the database. This is critical for reentrant runs
// where the schema generator may produce different family assignments than the
// original run.
func (db *DB) GetPendingSchemasWithFamily(ctx context.Context) ([]PendingSchema, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT si.schema_name, f.id, f.name, f.domain
		 FROM calibration.schema_instances si
		 JOIN calibration.families f ON f.id = si.family_id
		 WHERE NOT EXISTS (
			 SELECT 1 FROM calibration.results r WHERE r.schema_instance_id = si.id
		 )
		 ORDER BY si.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pending []PendingSchema
	for rows.Next() {
		var p PendingSchema
		if err := rows.Scan(&p.SchemaName, &p.FamilyID, &p.FamilyName, &p.Domain); err != nil {
			return nil, err
		}
		pending = append(pending, p)
	}
	return pending, rows.Err()
}

// GetSchemaWorkByName retrieves a schema instance's details by name.
func (db *DB) GetSchemaWorkByName(ctx context.Context, schemaName string) (familyID int, ddl string, isOptimal bool, err error) {
	err = db.conn.QueryRowContext(ctx,
		`SELECT family_id, ddl, is_optimal FROM calibration.schema_instances WHERE schema_name = $1`,
		schemaName).Scan(&familyID, &ddl, &isOptimal)
	return
}

// GetFamilyDomain returns the domain name for a family.
func (db *DB) GetFamilyDomain(ctx context.Context, familyID int) (string, error) {
	var domain string
	err := db.conn.QueryRowContext(ctx,
		`SELECT domain FROM calibration.families WHERE id = $1`, familyID).Scan(&domain)
	return domain, err
}

// CountTotalResults returns total number of EXPLAIN results collected.
func (db *DB) CountTotalResults(ctx context.Context) (int, error) {
	var count int
	err := db.conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM calibration.results`).Scan(&count)
	return count, err
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
func (db *DB) InsertResult(ctx context.Context, r *ScoredResult, batchID int) error {
	findingsArr := "{" + strings.Join(r.Findings, ",") + "}"
	_, err := db.conn.ExecContext(ctx,
		`INSERT INTO calibration.results
		 (batch_id, query_id, schema_instance_id, plan, total_cost, startup_cost, actual_time_ms,
		  rows_planned, rows_actual, shared_hit_blocks, shared_read_blocks,
		  score_total, score_efficiency, score_memory_compute, score_cognitive, findings)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16::text[])`,
		batchID, r.QueryID, r.SchemaInstanceID, r.Plan, r.TotalCost, r.StartupCost, r.ActualTimeMs,
		r.RowsPlanned, r.RowsActual, r.SharedHitBlocks, r.SharedReadBlocks,
		r.ScoreTotal, r.ScoreEfficiency, r.ScoreMemory, r.ScoreCognitive, findingsArr,
	)
	return err
}

// QueryTypePairs maps antipattern query types to their control counterparts.
// The weight for a rule is derived from cost(antipattern) / cost(control).
var QueryTypePairs = map[string]string{
	"select_star":        "select_columns",
	"non_sargable":       "sargable",
	"unbounded_sort":     "bounded_sort",
	"window_no_partition": "window_partitioned",
	"exists_subquery":    "proper_join",
	"in_subquery":        "proper_join",
	"distinct_join":      "proper_join",
	"cartesian":          "proper_join",
	"group_by":           "select_columns",
	"cte":                "select_columns",
	"union":              "select_columns",
	"boolean_nesting":    "sargable",
	"case_expr":          "select_columns",
}

// LoadResultsForRegression loads paired results: antipattern queries vs control queries
// running on the same schema. The cost ratio between them measures the antipattern's impact.
func (db *DB) LoadResultsForRegression(ctx context.Context) ([]RegressionRow, error) {
	// Get average cost per (query_type, schema_instance, family)
	query := `
		WITH avg_costs AS (
			SELECT q.family_id, q.query_type, r.schema_instance_id,
				   AVG(r.total_cost) AS avg_cost,
				   AVG(r.actual_time_ms) AS avg_time,
				   q.target_rules,
				   r.findings
			FROM calibration.results r
			JOIN calibration.queries q ON r.query_id = q.id
			WHERE r.total_cost > 0
			GROUP BY q.family_id, q.query_type, r.schema_instance_id, q.target_rules, r.findings
		)
		SELECT family_id, query_type, schema_instance_id, avg_cost, avg_time, target_rules, findings
		FROM avg_costs
		ORDER BY family_id, schema_instance_id, query_type
	`

	rows, err := db.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Collect by (family, schema) → query_type → cost
	type key struct {
		familyID int
		schemaID int
	}
	type costEntry struct {
		avgCost  float64
		avgTime  float64
		rules    []string
		findings []string
	}
	costs := make(map[key]map[string]costEntry)

	for rows.Next() {
		var familyID, schemaID int
		var queryType string
		var avgCost, avgTime float64
		var rulesStr, findingsStr string
		if err := rows.Scan(&familyID, &queryType, &schemaID, &avgCost, &avgTime, &rulesStr, &findingsStr); err != nil {
			return nil, err
		}
		k := key{familyID, schemaID}
		if costs[k] == nil {
			costs[k] = make(map[string]costEntry)
		}
		costs[k][queryType] = costEntry{avgCost, avgTime, parseArray(rulesStr), parseArray(findingsStr)}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Build regression rows by pairing antipattern with control
	var results []RegressionRow
	for _, typeCosts := range costs {
		for antiType, controlType := range QueryTypePairs {
			anti, hasAnti := typeCosts[antiType]
			control, hasControl := typeCosts[controlType]
			if !hasAnti || !hasControl || control.avgCost <= 0 {
				continue
			}
			costRatio := anti.avgCost / control.avgCost
			timeRatio := 0.0
			if control.avgTime > 0 {
				timeRatio = anti.avgTime / control.avgTime
			}
			results = append(results, RegressionRow{
				CostRatio: costRatio,
				TimeRatio: timeRatio,
				Findings:  anti.findings,
				Mutations: anti.rules,
			})
		}
	}

	return results, nil
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
