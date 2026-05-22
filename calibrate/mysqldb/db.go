// Package mysqldb provides the MySQL implementation of calibrate.DialectDB.
package mysqldb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"regexp"

	"github.com/sam-caldwell/query-test-tool/calibrate"
)

// DB implements calibrate.DialectDB for MySQL.
type DB struct {
	conn       *sql.DB
	cfg        calibrate.PipelineConfig
	debugCount int
}

// NewDB creates a new MySQL database connection.
func NewDB(cfg calibrate.PipelineConfig) (*DB, error) {
	// Append MySQL connection parameters. Use ? if no params exist, & otherwise.
	dsn := cfg.DSN
	if strings.Contains(dsn, "?") {
		dsn += "&parseTime=true&multiStatements=true&interpolateParams=true"
	} else {
		dsn += "?parseTime=true&multiStatements=true&interpolateParams=true"
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("connecting to MySQL: %w", err)
	}

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("pinging MySQL: %w", err)
	}

	// Check max_connections
	var maxConn int
	if err := db.QueryRowContext(ctx, "SELECT @@max_connections").Scan(&maxConn); err == nil {
		workers := cfg.Workers
		if workers == 0 {
			workers = 24
		}
		log.Printf("MySQL max_connections=%d (need %d workers + overhead)", maxConn, workers)
	}

	db.SetMaxOpenConns(cfg.Workers + 15)
	db.SetMaxIdleConns(cfg.Workers)

	// Set session variables for performance
	db.Exec("SET SESSION cte_max_recursion_depth = 100000")

	return &DB{conn: db, cfg: cfg}, nil
}

func (db *DB) Conn() *sql.DB { return db.conn }
func (db *DB) Close() error  { return db.conn.Close() }

func (db *DB) InitTrackingTables(ctx context.Context) error {
	ddl := `
	CREATE TABLE IF NOT EXISTS calibration_families (
		id INT AUTO_INCREMENT PRIMARY KEY,
		domain VARCHAR(100) NOT NULL,
		name VARCHAR(200) NOT NULL UNIQUE,
		description TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB;

	CREATE TABLE IF NOT EXISTS calibration_schema_instances (
		id INT AUTO_INCREMENT PRIMARY KEY,
		family_id INT NOT NULL,
		schema_name VARCHAR(200) NOT NULL UNIQUE,
		is_optimal TINYINT(1) NOT NULL DEFAULT 0,
		mutations JSON,
		ddl MEDIUMTEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (family_id) REFERENCES calibration_families(id)
	) ENGINE=InnoDB;

	CREATE TABLE IF NOT EXISTS calibration_queries (
		id INT AUTO_INCREMENT PRIMARY KEY,
		family_id INT NOT NULL,
		sql_text MEDIUMTEXT NOT NULL,
		query_type VARCHAR(50) NOT NULL,
		target_rules JSON,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (family_id) REFERENCES calibration_families(id)
	) ENGINE=InnoDB;

	CREATE TABLE IF NOT EXISTS calibration_results (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		batch_id INT NOT NULL DEFAULT 0,
		query_id INT NOT NULL,
		schema_instance_id INT NOT NULL,
		plan JSON,
		total_cost DOUBLE,
		startup_cost DOUBLE,
		actual_time_ms DOUBLE,
		rows_planned BIGINT,
		rows_actual BIGINT,
		shared_hit_blocks BIGINT DEFAULT 0,
		shared_read_blocks BIGINT DEFAULT 0,
		score_total INT,
		score_efficiency INT,
		score_memory_compute INT,
		score_cognitive INT,
		findings JSON,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_results_schema (schema_instance_id),
		INDEX idx_results_query (query_id),
		INDEX idx_results_batch (batch_id)
	) ENGINE=InnoDB;
	`
	_, err := db.conn.ExecContext(ctx, ddl)
	return err
}

func (db *DB) CreateResultPartitions(ctx context.Context, totalBatches int) error {
	// MySQL doesn't use the same partitioning approach as PG.
	// The single results table with indexes is sufficient.
	return nil
}

func (db *DB) InsertFamily(ctx context.Context, domain, name, description string) (int, error) {
	res, err := db.conn.ExecContext(ctx,
		"INSERT INTO calibration_families (domain, name, description) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE description = VALUES(description)",
		domain, name, description)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		// ON DUPLICATE KEY doesn't return LastInsertId reliably — query it
		var existingID int
		err = db.conn.QueryRowContext(ctx, "SELECT id FROM calibration_families WHERE name = ?", name).Scan(&existingID)
		return existingID, err
	}
	if id == 0 {
		var existingID int
		err = db.conn.QueryRowContext(ctx, "SELECT id FROM calibration_families WHERE name = ?", name).Scan(&existingID)
		return existingID, err
	}
	return int(id), nil
}

func (db *DB) InsertSchemaInstance(ctx context.Context, familyID int, schemaName string, isOptimal bool, mutations []string, ddl string) (int, error) {
	mutJSON, _ := json.Marshal(mutations)
	optimal := 0
	if isOptimal {
		optimal = 1
	}
	res, err := db.conn.ExecContext(ctx,
		"INSERT INTO calibration_schema_instances (family_id, schema_name, is_optimal, mutations, ddl) VALUES (?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE ddl = VALUES(ddl)",
		familyID, schemaName, optimal, string(mutJSON), ddl)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		var existingID int
		err = db.conn.QueryRowContext(ctx, "SELECT id FROM calibration_schema_instances WHERE schema_name = ?", schemaName).Scan(&existingID)
		return existingID, err
	}
	return int(id), nil
}

func (db *DB) ApplySchema(ctx context.Context, schemaName, ddl string) error {
	// Split DDL into individual statements and execute each
	stmts := strings.Split(ddl, ";\n")
	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.conn.ExecContext(ctx, stmt); err != nil {
			// Try to clean up on failure — drop all prefixed tables
			db.dropPrefixedTables(ctx, schemaName)
			return fmt.Errorf("applying DDL: %w", err)
		}
	}
	return nil
}

func (db *DB) dropPrefixedTables(ctx context.Context, prefix string) {
	rows, err := db.conn.QueryContext(ctx,
		"SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME LIKE ?",
		prefix+"_%")
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		db.conn.ExecContext(ctx, "SET foreign_key_checks = 0")
		db.conn.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS `%s`", name))
		db.conn.ExecContext(ctx, "SET foreign_key_checks = 1")
	}
}

func (db *DB) ApplyIndexesAndFKs(ctx context.Context, ddl string) error {
	stmts := strings.Split(ddl, ";\n")
	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.conn.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) DropOrphanSchemas(ctx context.Context) (int, error) {
	// Find all cal_NNNNN prefixed tables and drop them (batch-and-drop cleanup).
	// Uses table prefix pattern instead of separate databases.
	rows, err := db.conn.QueryContext(ctx,
		`SELECT TABLE_NAME FROM information_schema.TABLES
		 WHERE TABLE_SCHEMA = DATABASE()
		 AND TABLE_NAME REGEXP '^cal_[0-9]{5}_'`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		tables = append(tables, name)
	}

	if len(tables) == 0 {
		return 0, nil
	}

	// Disable FK checks for faster drops
	db.conn.ExecContext(ctx, "SET foreign_key_checks = 0")
	defer db.conn.ExecContext(ctx, "SET foreign_key_checks = 1")

	dropped := 0
	for _, name := range tables {
		if _, err := db.conn.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS `%s`", name)); err != nil {
			log.Printf("Warning: failed to drop table %s: %v", name, err)
			continue
		}
		dropped++
	}

	return dropped, nil
}

func (db *DB) InsertQueryBatch(ctx context.Context, queries []calibrate.GeneratedQuery) error {
	if len(queries) == 0 {
		return nil
	}
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		"INSERT INTO calibration_queries (family_id, sql_text, query_type, target_rules) VALUES (?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, q := range queries {
		rulesJSON, _ := json.Marshal(q.TargetRules)
		if _, err := stmt.ExecContext(ctx, q.FamilyID, q.SQL, q.QueryType, string(rulesJSON)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) CountQueriesForFamily(ctx context.Context, familyID int) (int, error) {
	var count int
	err := db.conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM calibration_queries WHERE family_id = ?", familyID).Scan(&count)
	return count, err
}

func (db *DB) GetExistingFamilies(ctx context.Context) (map[string]int, error) {
	rows, err := db.conn.QueryContext(ctx, "SELECT id, name FROM calibration_families")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	families := make(map[string]int)
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		families[name] = id
	}
	return families, rows.Err()
}

func (db *DB) CountSchemaInstancesForFamily(ctx context.Context, familyID int) (int, error) {
	var count int
	err := db.conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM calibration_schema_instances WHERE family_id = ?", familyID).Scan(&count)
	return count, err
}

func (db *DB) GetSchemasWithoutResults(ctx context.Context) ([]string, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT si.schema_name FROM calibration_schema_instances si
		 WHERE NOT EXISTS (
			 SELECT 1 FROM calibration_results r WHERE r.schema_instance_id = si.id
		 )
		 ORDER BY si.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (db *DB) GetPendingSchemasWithFamily(ctx context.Context) ([]calibrate.PendingSchema, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT si.schema_name, f.id, f.name, f.domain
		 FROM calibration_schema_instances si
		 JOIN calibration_families f ON f.id = si.family_id
		 WHERE NOT EXISTS (
			 SELECT 1 FROM calibration_results r WHERE r.schema_instance_id = si.id
		 )
		 ORDER BY si.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pending []calibrate.PendingSchema
	for rows.Next() {
		var p calibrate.PendingSchema
		if err := rows.Scan(&p.SchemaName, &p.FamilyID, &p.FamilyName, &p.Domain); err != nil {
			return nil, err
		}
		pending = append(pending, p)
	}
	return pending, rows.Err()
}

func (db *DB) GetFamilySchemas(ctx context.Context, familyID int) ([]calibrate.SchemaInstance, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, schema_name, is_optimal FROM calibration_schema_instances WHERE family_id = ? ORDER BY id`, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []calibrate.SchemaInstance
	for rows.Next() {
		var s calibrate.SchemaInstance
		var optimal int
		if err := rows.Scan(&s.ID, &s.SchemaName, &optimal); err != nil {
			return nil, err
		}
		s.IsOptimal = optimal == 1
		schemas = append(schemas, s)
	}
	return schemas, rows.Err()
}

func (db *DB) GetQueriesForFamily(ctx context.Context, familyID int) ([]calibrate.GeneratedQuery, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, family_id, sql_text, query_type FROM calibration_queries WHERE family_id = ?`, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []calibrate.GeneratedQuery
	for rows.Next() {
		var q calibrate.GeneratedQuery
		if err := rows.Scan(&q.ID, &q.FamilyID, &q.SQL, &q.QueryType); err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	return queries, rows.Err()
}

func (db *DB) RunExplain(ctx context.Context, schemaName, querySQL string) (*calibrate.ExplainResult, error) {
	// For MySQL, tables use prefix naming (cal_00001_users) within a single database.
	// The querySQL already references unprefixed table names from the archetype.
	// We need to rewrite table names to include the schema prefix.
	// For now, we use EXPLAIN FORMAT=JSON on the prefixed query.
	timeoutMs := db.cfg.StatementTimeout
	if timeoutMs == 0 {
		timeoutMs = 5000
	}

	// Rewrite table names in the query to use the schema prefix
	prefixedSQL := rewriteTableNames(querySQL, schemaName)
	if prefixedSQL == "" {
		return nil, fmt.Errorf("rewriteTableNames returned empty for: %s", querySQL[:min(50, len(querySQL))])
	}

	explainSQL := "EXPLAIN FORMAT=JSON " + prefixedSQL
	var planJSON string
	err := db.conn.QueryRowContext(ctx, explainSQL).Scan(&planJSON)
	if err != nil {
		// Log first few failures for debugging
		if db.debugCount < 10 {
			log.Printf("DEBUG EXPLAIN failed: schema=%s err=%v\n  original: %s\n  rewritten: %s", schemaName, err, querySQL[:min(100, len(querySQL))], prefixedSQL[:min(100, len(prefixedSQL))])
			db.debugCount++
		}
		return nil, fmt.Errorf("EXPLAIN failed: %w", err)
	}

	return parseMySQLExplainJSON([]byte(planJSON))
}

// rewriteTableNames rewrites all table references in a query to use the schema prefix.
// Since the query generator produces queries with known table names from archetypes,
// we use word-boundary regex replacement for each known table name.
func rewriteTableNames(sql, prefix string) string {
	// Collect all known table names from all archetypes, sorted longest first
	// to prevent partial replacements (e.g., "users" matching inside "user_roles")
	var tableNames []string
	seen := make(map[string]bool)
	for _, arch := range calibrate.Archetypes() {
		for _, table := range arch.Tables {
			if !seen[table.Name] {
				tableNames = append(tableNames, table.Name)
				seen[table.Name] = true
			}
		}
	}
	// Sort longest first
	for i := 0; i < len(tableNames); i++ {
		for j := i + 1; j < len(tableNames); j++ {
			if len(tableNames[j]) > len(tableNames[i]) {
				tableNames[i], tableNames[j] = tableNames[j], tableNames[i]
			}
		}
	}

	result := sql
	for _, name := range tableNames {
		prefixed := prefix + "_" + name
		// Use a word-boundary aware replacement via regexp
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
		result = re.ReplaceAllString(result, prefixed)
	}
	return result
}

func parseMySQLExplainJSON(data []byte) (*calibrate.ExplainResult, error) {
	// MySQL 9.x uses json_schema_version 2.0 with query_plan.estimated_total_cost
	// Older MySQL uses query_block.cost_info.query_cost
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing EXPLAIN JSON: %w", err)
	}

	var cost float64

	// Try MySQL 9.x format (json_schema_version 2.0)
	if planData, ok := raw["query_plan"]; ok {
		var plan struct {
			EstimatedTotalCost float64 `json:"estimated_total_cost"`
			EstimatedRows      float64 `json:"estimated_rows"`
		}
		if err := json.Unmarshal(planData, &plan); err == nil && plan.EstimatedTotalCost > 0 {
			cost = plan.EstimatedTotalCost
		}
	}

	// Fallback: MySQL 8.x format (query_block.cost_info.query_cost)
	if cost == 0 {
		if blockData, ok := raw["query_block"]; ok {
			var block struct {
				CostInfo struct {
					QueryCost string `json:"query_cost"`
				} `json:"cost_info"`
			}
			if err := json.Unmarshal(blockData, &block); err == nil {
				fmt.Sscanf(block.CostInfo.QueryCost, "%f", &cost)
			}
		}
	}

	return &calibrate.ExplainResult{
		Plan:      data,
		TotalCost: cost,
	}, nil
}

func (db *DB) InsertResult(ctx context.Context, r *calibrate.ScoredResult, batchID int) error {
	findingsJSON, _ := json.Marshal(r.Findings)
	_, err := db.conn.ExecContext(ctx,
		`INSERT INTO calibration_results
		 (batch_id, query_id, schema_instance_id, plan, total_cost, startup_cost, actual_time_ms,
		  rows_planned, rows_actual, shared_hit_blocks, shared_read_blocks,
		  score_total, score_efficiency, score_memory_compute, score_cognitive, findings)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		batchID, r.QueryID, r.SchemaInstanceID, r.Plan, r.TotalCost, r.StartupCost, r.ActualTimeMs,
		r.RowsPlanned, r.RowsActual, r.SharedHitBlocks, r.SharedReadBlocks,
		r.ScoreTotal, r.ScoreEfficiency, r.ScoreMemory, r.ScoreCognitive, string(findingsJSON))
	return err
}

func (db *DB) CountTotalResults(ctx context.Context) (int, error) {
	var count int
	err := db.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM calibration_results").Scan(&count)
	return count, err
}

func (db *DB) LoadResultsForRegression(ctx context.Context) ([]calibrate.RegressionRow, error) {
	query := `
		SELECT q.family_id, q.query_type, r.schema_instance_id,
			AVG(r.total_cost) AS avg_cost,
			AVG(r.actual_time_ms) AS avg_time,
			q.target_rules,
			r.findings
		FROM calibration_results r
		JOIN calibration_queries q ON r.query_id = q.id
		WHERE r.total_cost > 0
		GROUP BY q.family_id, q.query_type, r.schema_instance_id, q.target_rules, r.findings
		ORDER BY q.family_id, r.schema_instance_id, q.query_type
	`

	rows, err := db.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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
		var rulesJSON, findingsJSON string
		if err := rows.Scan(&familyID, &queryType, &schemaID, &avgCost, &avgTime, &rulesJSON, &findingsJSON); err != nil {
			return nil, err
		}
		k := key{familyID, schemaID}
		if costs[k] == nil {
			costs[k] = make(map[string]costEntry)
		}
		costs[k][queryType] = costEntry{avgCost, avgTime, parseJSONArray(rulesJSON), parseJSONArray(findingsJSON)}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var results []calibrate.RegressionRow
	for _, typeCosts := range costs {
		for antiType, controlType := range calibrate.QueryTypePairs {
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
			results = append(results, calibrate.RegressionRow{
				CostRatio: costRatio,
				TimeRatio: timeRatio,
				Findings:  anti.findings,
				Mutations: anti.rules,
			})
		}
	}

	return results, nil
}

func parseJSONArray(s string) []string {
	if s == "" || s == "null" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		return nil
	}
	return arr
}
