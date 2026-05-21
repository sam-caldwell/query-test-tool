package calibrate

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func getTestDSN() string {
	dsn := os.Getenv("TEST_DSN")
	if dsn == "" {
		return ""
	}
	return dsn
}

// TestRunAllSchemaFilter verifies that RunAll only processes schemas in the filter.
func TestRunAllSchemaFilter(t *testing.T) {
	dsn := getTestDSN()
	if dsn == "" {
		t.Skip("TEST_DSN not set — skipping integration test")
	}

	cfg := DefaultConfig()
	cfg.DSN = dsn
	cfg.StatementTimeout = 5000
	db, err := NewDB(cfg)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Setup: create tracking tables
	if err := db.InitTrackingTables(ctx); err != nil {
		t.Fatalf("InitTrackingTables: %v", err)
	}

	// Create partitions for batch 0 and 1
	if err := db.CreateResultPartitions(ctx, 1); err != nil {
		t.Fatalf("CreateResultPartitions: %v", err)
	}

	// Insert a family
	domain := Archetypes()[0]
	familyID, err := db.InsertFamily(ctx, domain.Name, "test_filter_family", "test")
	if err != nil {
		t.Fatalf("InsertFamily: %v", err)
	}

	// Create two schemas: one in filter, one not
	schemaIn := "cal_test_in"
	schemaOut := "cal_test_out"

	ddl := GenerateDDL(domain, schemaIn)
	if _, err := db.InsertSchemaInstance(ctx, familyID, schemaIn, true, nil, ddl); err != nil {
		t.Fatalf("InsertSchemaInstance (in): %v", err)
	}
	if err := db.ApplySchema(ctx, schemaIn, ddl); err != nil {
		t.Fatalf("ApplySchema (in): %v", err)
	}

	ddlOut := GenerateDDL(domain, schemaOut)
	if _, err := db.InsertSchemaInstance(ctx, familyID, schemaOut, false, nil, ddlOut); err != nil {
		t.Fatalf("InsertSchemaInstance (out): %v", err)
	}
	if err := db.ApplySchema(ctx, schemaOut, ddlOut); err != nil {
		t.Fatalf("ApplySchema (out): %v", err)
	}

	// Insert a simple query
	_, err = db.InsertQuery(ctx, familyID, fmt.Sprintf("SELECT 1 FROM %s.businesses LIMIT 1", schemaIn), "select_columns", nil)
	if err != nil {
		t.Fatalf("InsertQuery: %v", err)
	}

	// Run with filter — only schemaIn
	runner := NewRunner(db, cfg, "postgresql")
	filter := map[string]bool{schemaIn: true}
	families := []SchemaFamily{{ID: familyID, Domain: domain.Name, Name: "test_filter_family"}}

	err = runner.RunAll(ctx, families, 1, filter, nil)
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}

	// Verify: results should only exist for schemaIn
	hasIn, err := db.SchemaHasResults(ctx, schemaIn)
	if err != nil {
		t.Fatalf("SchemaHasResults (in): %v", err)
	}
	hasOut, err := db.SchemaHasResults(ctx, schemaOut)
	if err != nil {
		t.Fatalf("SchemaHasResults (out): %v", err)
	}

	if !hasIn {
		t.Error("expected schemaIn to have results")
	}
	if hasOut {
		t.Error("expected schemaOut to NOT have results (filtered out)")
	}

	// Cleanup
	db.conn.ExecContext(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaIn))
	db.conn.ExecContext(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaOut))
}

// TestDropOrphanSchemas verifies orphan cleanup drops cal_* schemas but not calibration.
func TestDropOrphanSchemas(t *testing.T) {
	dsn := getTestDSN()
	if dsn == "" {
		t.Skip("TEST_DSN not set — skipping integration test")
	}

	cfg := DefaultConfig()
	cfg.DSN = dsn
	db, err := NewDB(cfg)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Ensure calibration schema exists
	if err := db.InitTrackingTables(ctx); err != nil {
		t.Fatalf("InitTrackingTables: %v", err)
	}

	// Create some orphan schemas
	orphanNames := []string{"cal_test_orphan1", "cal_test_orphan2", "cal_test_orphan3"}
	for _, name := range orphanNames {
		_, err := db.conn.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", name))
		if err != nil {
			t.Fatalf("creating orphan %s: %v", name, err)
		}
	}

	// Verify orphans exist
	for _, name := range orphanNames {
		var exists bool
		db.conn.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)", name).Scan(&exists)
		if !exists {
			t.Fatalf("orphan %s should exist before cleanup", name)
		}
	}

	// Run cleanup
	dropped, err := db.DropOrphanSchemas(ctx)
	if err != nil {
		t.Fatalf("DropOrphanSchemas: %v", err)
	}

	if dropped < len(orphanNames) {
		t.Errorf("expected at least %d dropped, got %d", len(orphanNames), dropped)
	}

	// Verify orphans are gone
	for _, name := range orphanNames {
		var exists bool
		db.conn.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)", name).Scan(&exists)
		if exists {
			t.Errorf("orphan %s should not exist after cleanup", name)
		}
	}

	// Verify calibration schema still exists
	var calExists bool
	db.conn.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = 'calibration')").Scan(&calExists)
	if !calExists {
		t.Error("calibration schema should NOT be dropped by orphan cleanup")
	}
}

// TestDropOrphanSchemasAfterBatch verifies that ALL cal_* schemas are cleaned after a batch,
// not just the ones that successfully populated.
func TestDropOrphanSchemasAfterBatch(t *testing.T) {
	dsn := getTestDSN()
	if dsn == "" {
		t.Skip("TEST_DSN not set — skipping integration test")
	}

	cfg := DefaultConfig()
	cfg.DSN = dsn
	db, err := NewDB(cfg)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	if err := db.InitTrackingTables(ctx); err != nil {
		t.Fatalf("InitTrackingTables: %v", err)
	}

	// Simulate: batch creates 3 schemas, but only 2 succeed (createdNames has 2)
	// The 3rd is an orphan that failed during populate
	allSchemas := []string{"cal_test_batch_ok1", "cal_test_batch_ok2", "cal_test_batch_fail"}
	for _, name := range allSchemas {
		_, err := db.conn.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", name))
		if err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}

	// DropOrphanSchemas should drop ALL of them
	dropped, err := db.DropOrphanSchemas(ctx)
	if err != nil {
		t.Fatalf("DropOrphanSchemas: %v", err)
	}

	if dropped < 3 {
		t.Errorf("expected at least 3 dropped, got %d", dropped)
	}

	// Verify all are gone
	for _, name := range allSchemas {
		var exists bool
		db.conn.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)", name).Scan(&exists)
		if exists {
			t.Errorf("%s should not exist after DropOrphanSchemas", name)
		}
	}
}

// TestDropOrphanSchemasWithAutovacuum verifies that orphan schemas get dropped even
// when autovacuum is actively running on them. This was the root cause of orphan
// accumulation that degraded calibration performance.
func TestDropOrphanSchemasWithAutovacuum(t *testing.T) {
	dsn := getTestDSN()
	if dsn == "" {
		t.Skip("TEST_DSN not set — skipping integration test")
	}

	cfg := DefaultConfig()
	cfg.DSN = dsn
	cfg.RowsPerTable = 1000 // small but enough to trigger autovacuum
	db, err := NewDB(cfg)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	if err := db.InitTrackingTables(ctx); err != nil {
		t.Fatalf("InitTrackingTables: %v", err)
	}

	// Create a schema with enough data to attract autovacuum
	schemaName := "cal_test_autovac"
	domain := Archetypes()[0]
	tablesDDL := GenerateDDLTablesOnly(domain, schemaName)
	if err := db.ApplySchema(ctx, schemaName, tablesDDL); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	// Insert some data to make autovacuum interested
	for _, table := range domain.Tables {
		db.conn.ExecContext(ctx, fmt.Sprintf(
			"INSERT INTO %s.%s SELECT generate_series(1, 100)", schemaName, table.Name))
	}

	// Delete data to create dead tuples (triggers autovacuum)
	for _, table := range domain.Tables {
		db.conn.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s.%s", schemaName, table.Name))
	}

	// Now drop — this must succeed even if autovacuum is running
	dropped, err := db.DropOrphanSchemas(ctx)
	if err != nil {
		t.Fatalf("DropOrphanSchemas: %v", err)
	}
	if dropped < 1 {
		t.Errorf("expected at least 1 dropped, got %d", dropped)
	}

	// Verify it's actually gone
	var exists bool
	db.conn.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)",
		schemaName).Scan(&exists)
	if exists {
		t.Errorf("%s should not exist after DropOrphanSchemas — autovacuum deadlock prevention failed", schemaName)
	}
}

// TestDropOrphanSchemasRetry verifies that the retry logic handles transient failures.
func TestDropOrphanSchemasRetry(t *testing.T) {
	dsn := getTestDSN()
	if dsn == "" {
		t.Skip("TEST_DSN not set — skipping integration test")
	}

	cfg := DefaultConfig()
	cfg.DSN = dsn
	db, err := NewDB(cfg)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	if err := db.InitTrackingTables(ctx); err != nil {
		t.Fatalf("InitTrackingTables: %v", err)
	}

	// Create multiple orphan schemas
	names := []string{"cal_test_retry1", "cal_test_retry2", "cal_test_retry3", "cal_test_retry4", "cal_test_retry5"}
	for _, name := range names {
		_, err := db.conn.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", name))
		if err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
		// Add a table so DROP CASCADE has work to do
		db.conn.ExecContext(ctx, fmt.Sprintf("CREATE TABLE %s.test_table (id serial, data text)", name))
		db.conn.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s.test_table (data) SELECT md5(random()::text) FROM generate_series(1, 100)", name))
	}

	// Drop all orphans
	dropped, err := db.DropOrphanSchemas(ctx)
	if err != nil {
		t.Fatalf("DropOrphanSchemas: %v", err)
	}
	if dropped < len(names) {
		t.Errorf("expected %d dropped, got %d", len(names), dropped)
	}

	// Verify ALL are gone — the verification step in DropOrphanSchemas should catch any remaining
	for _, name := range names {
		var exists bool
		db.conn.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)", name).Scan(&exists)
		if exists {
			t.Errorf("%s should not exist after DropOrphanSchemas", name)
		}
	}
}

// TestDropOrphanSchemasPreservesCalibration verifies the calibration schema is never dropped.
func TestDropOrphanSchemasPreservesCalibration(t *testing.T) {
	dsn := getTestDSN()
	if dsn == "" {
		t.Skip("TEST_DSN not set — skipping integration test")
	}

	cfg := DefaultConfig()
	cfg.DSN = dsn
	db, err := NewDB(cfg)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	if err := db.InitTrackingTables(ctx); err != nil {
		t.Fatalf("InitTrackingTables: %v", err)
	}

	// Create an orphan
	db.conn.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS cal_test_preserve")

	// Drop orphans
	db.DropOrphanSchemas(ctx)

	// Verify calibration schema and its tables still exist
	var calExists bool
	db.conn.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = 'calibration')").Scan(&calExists)
	if !calExists {
		t.Fatal("calibration schema was dropped — CRITICAL BUG")
	}

	var tablesExist bool
	db.conn.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = 'calibration' AND table_name = 'families')").Scan(&tablesExist)
	if !tablesExist {
		t.Fatal("calibration.families was dropped — CRITICAL BUG")
	}
}

// TestBatchPartitionIsolation verifies that results go into the correct partition.
func TestBatchPartitionIsolation(t *testing.T) {
	dsn := getTestDSN()
	if dsn == "" {
		t.Skip("TEST_DSN not set — skipping integration test")
	}

	cfg := DefaultConfig()
	cfg.DSN = dsn
	cfg.StatementTimeout = 5000
	db, err := NewDB(cfg)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	if err := db.InitTrackingTables(ctx); err != nil {
		t.Fatalf("InitTrackingTables: %v", err)
	}

	// Create partitions for batches 0-5
	if err := db.CreateResultPartitions(ctx, 5); err != nil {
		t.Fatalf("CreateResultPartitions: %v", err)
	}

	// Insert a family and schema
	domain := Archetypes()[0]
	familyID, err := db.InsertFamily(ctx, domain.Name, "test_partition_family", "test")
	if err != nil {
		t.Fatalf("InsertFamily: %v", err)
	}

	schemaID := 0
	if id, err := db.InsertSchemaInstance(ctx, familyID, "cal_test_part", true, nil, ""); err != nil {
		t.Fatalf("InsertSchemaInstance: %v", err)
	} else {
		schemaID = id
	}

	queryID := 0
	if id, err := db.InsertQuery(ctx, familyID, "SELECT 1", "select_columns", nil); err != nil {
		t.Fatalf("InsertQuery: %v", err)
	} else {
		queryID = id
	}

	// Insert results into batch 3
	result := &ScoredResult{
		ExplainResult: ExplainResult{
			TotalCost:   100.0,
			StartupCost: 10.0,
		},
		ScoreTotal:      50,
		ScoreEfficiency: 20,
		ScoreMemory:     15,
		ScoreCognitive:  15,
		Findings:        []string{},
	}
	result.QueryID = queryID
	result.SchemaInstanceID = schemaID

	if err := db.InsertResult(ctx, result, 3); err != nil {
		t.Fatalf("InsertResult batch 3: %v", err)
	}

	// Insert result into batch 5
	if err := db.InsertResult(ctx, result, 5); err != nil {
		t.Fatalf("InsertResult batch 5: %v", err)
	}

	// Verify results are in correct partitions
	var countB3, countB5, countTotal int
	db.conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM calibration.results_b0003 WHERE batch_id = 3").Scan(&countB3)
	db.conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM calibration.results_b0005 WHERE batch_id = 5").Scan(&countB5)
	db.conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM calibration.results WHERE query_id = $1", queryID).Scan(&countTotal)

	if countB3 < 1 {
		t.Error("expected at least 1 result in partition b0003")
	}
	if countB5 < 1 {
		t.Error("expected at least 1 result in partition b0005")
	}
	if countTotal < 2 {
		t.Errorf("expected at least 2 total results, got %d", countTotal)
	}
}

// TestGenerateDDLTablesOnly verifies UNLOGGED tables and no indexes.
func TestGenerateDDLTablesOnly(t *testing.T) {
	domain := Archetypes()[0]
	ddl := GenerateDDLTablesOnly(domain, "test_schema")

	if !contains(ddl, "CREATE SCHEMA test_schema") {
		t.Error("should contain CREATE SCHEMA")
	}
	if !contains(ddl, "CREATE UNLOGGED TABLE") {
		t.Error("should use UNLOGGED tables")
	}
	if contains(ddl, "CREATE INDEX") {
		t.Error("should NOT contain indexes")
	}
	if contains(ddl, "ALTER TABLE") {
		t.Error("should NOT contain foreign keys")
	}
}

// TestGenerateDDLIndexesAndFKs verifies indexes and FKs only.
func TestGenerateDDLIndexesAndFKs(t *testing.T) {
	domain := Archetypes()[0]
	ddl := GenerateDDLIndexesAndFKs(domain, "test_schema")

	if contains(ddl, "CREATE TABLE") {
		t.Error("should NOT contain table definitions")
	}
	if contains(ddl, "CREATE SCHEMA") {
		t.Error("should NOT contain schema creation")
	}
	if !contains(ddl, "CREATE INDEX") {
		t.Error("should contain indexes")
	}
	if !contains(ddl, "ALTER TABLE") {
		t.Error("should contain foreign keys")
	}
}

// TestGenerateDDLSplitEquivalence verifies that tables-only + indexes-and-FKs
// produces the same logical schema as the full DDL.
func TestGenerateDDLSplitEquivalence(t *testing.T) {
	domain := Archetypes()[0]
	schema := "test_equiv"

	full := GenerateDDL(domain, schema)
	tablesOnly := GenerateDDLTablesOnly(domain, schema)
	indexesOnly := GenerateDDLIndexesAndFKs(domain, schema)

	// Full DDL should contain everything that's in both splits
	for _, idx := range domain.Indexes {
		idxName := idx.Name
		if !contains(full, idxName) {
			t.Errorf("full DDL missing index %s", idxName)
		}
		if !contains(indexesOnly, idxName) {
			t.Errorf("indexes DDL missing index %s", idxName)
		}
		if contains(tablesOnly, idxName) {
			t.Errorf("tables-only DDL should not contain index %s", idxName)
		}
	}

	for _, tbl := range domain.Tables {
		tblName := schema + "." + tbl.Name
		if !contains(full, tblName) {
			t.Errorf("full DDL missing table %s", tblName)
		}
		if !contains(tablesOnly, tblName) {
			t.Errorf("tables-only DDL missing table %s", tblName)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && stringContains(s, substr)
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
