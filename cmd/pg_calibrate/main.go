package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"syscall"
	"strings"

	"github.com/sam-caldwell/query-test-tool/src/calibrate"
)

// enforceSingleInstance uses a PID file lock to ensure only one calibrate
// instance runs at a time. This prevents connection saturation and duplicate
// data in the results table.
func enforceSingleInstance() func() {
	pidFile := os.TempDir() + "/calibrate.pid"

	// Check for existing PID file
	if data, err := os.ReadFile(pidFile); err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil && pid != os.Getpid() {
			// Check if that process is still alive
			if process, err := os.FindProcess(pid); err == nil {
				if err := process.Signal(syscall.Signal(0)); err == nil {
					log.Fatalf("FATAL: another calibrate instance is already running (PID %d). "+
						"Only one instance may run at a time to prevent connection saturation and duplicate data. "+
						"Kill the existing instance first, or remove %s if the process is gone.", pid, pidFile)
				}
			}
		}
		// Stale PID file — process is dead
		os.Remove(pidFile)
	}

	// Write our PID
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		log.Fatalf("FATAL: cannot write PID file %s: %v", pidFile, err)
	}

	// Return cleanup function
	return func() { os.Remove(pidFile) }
}

func main() {
	cleanupPID := enforceSingleInstance()
	defer cleanupPID()

	var (
		dsn          string
		dbHost       string
		dbPort       int
		dbUser       string
		dbPassword   string
		dbName       string
		dbSSLMode    string
		phase        string
		schemas      int
		queries      int
		rows         int
		workers      int
		batchSize    int
		timeout      int
		outputFile   string
		logFile      string
		schemaFile   string
	)

	flag.StringVar(&dsn, "dsn", "", "Full PostgreSQL connection string (overrides individual -host/-port/-user/-password/-dbname flags)")
	flag.StringVar(&dbHost, "host", "localhost", "PostgreSQL host")
	flag.IntVar(&dbPort, "port", 5432, "PostgreSQL port")
	flag.StringVar(&dbUser, "user", "", "PostgreSQL user")
	flag.StringVar(&dbPassword, "password", "", "PostgreSQL password")
	flag.StringVar(&dbName, "dbname", "sqlscore_calibrate", "PostgreSQL database name")
	flag.StringVar(&dbSSLMode, "sslmode", "disable", "PostgreSQL SSL mode (disable, require, verify-ca, verify-full)")
	flag.StringVar(&phase, "phase", "all", "Pipeline phase: init, generate, run, calculate, or all")
	defaultCfg := calibrate.DefaultConfig()
	flag.IntVar(&schemas, "schemas", defaultCfg.SchemaCount, "Target number of schema variants to generate")
	flag.IntVar(&queries, "queries", defaultCfg.QueryCount, "Target number of queries to generate")
	flag.IntVar(&rows, "rows", defaultCfg.RowsPerTable, "Base rows per table (tiered multipliers apply: 3× child, 5× hub, 10× high-volume)")
	defaultWorkers := defaultCfg.Workers
	flag.IntVar(&workers, "workers", defaultWorkers, fmt.Sprintf("Concurrent workers (auto-detected: %d = %d CPUs × 3)", defaultWorkers, defaultWorkers/3))
	flag.IntVar(&batchSize, "batch-size", defaultCfg.BatchSize, "Schemas per batch in batch-and-drop mode (0 to disable)")
	flag.IntVar(&timeout, "timeout", 5000, "Per-query statement timeout (ms)")
	flag.StringVar(&logFile, "logfile", "", "Log file path (use .gz extension for gzip compression)")
	flag.StringVar(&outputFile, "output", "src/scorer/weights.json", "Output file for calculated weights (embedded by cmd/query-test-tool at build time)")
	flag.StringVar(&schemaFile, "schema-file", "", "Path to a .SQL DDL file to import as an additional calibration domain")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `query-test-tool calibrate — Weight calibration via EXPLAIN ANALYZE

Generates schemas with known antipatterns, runs 1M queries against PostgreSQL,
and uses linear regression to determine optimal scoring weights.

Prerequisites:
  - PostgreSQL server running locally
  - Database created: createdb sqlscore_calibrate

Usage:
  calibrate [options]

Phases:
  init       Create tracking tables in the database
  generate   Generate 10K schemas and 1M queries, populate with data
  run        Execute EXPLAIN ANALYZE on all queries
  calculate  Run OLS regression to compute calibrated weights
  all        Run the complete pipeline (default)

Options:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  calibrate -dsn "postgres://user:pass@localhost:5432/sqlscore_calibrate?sslmode=disable"
  calibrate -phase generate -schemas 1000 -queries 100000 -rows 500
  calibrate -phase calculate -output weights.json
  calibrate -workers 16 -timeout 10000
`)
	}

	flag.Parse()

	// Set up log output
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Fatalf("Failed to open log file: %v", err)
		}
		defer f.Close()

		var logWriter io.Writer
		if strings.HasSuffix(logFile, ".gz") {
			gz := gzip.NewWriter(f)
			defer gz.Close()
			logWriter = gz
		} else {
			logWriter = f
		}
		// Write to both file and stderr so we can monitor via DB queries
		// while still having a compressed log for debugging
		log.SetOutput(io.MultiWriter(os.Stderr, logWriter))
	}

	cfg := calibrate.DefaultConfig()
	if dsn != "" {
		cfg.DSN = dsn
	} else if dbUser != "" {
		// Build DSN from individual flags
		cfg.DSN = fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
			dbUser, dbPassword, dbHost, dbPort, dbName, dbSSLMode)
	}
	cfg.SchemaCount = schemas
	cfg.QueryCount = queries
	cfg.RowsPerTable = rows
	cfg.Workers = workers
	cfg.BatchSize = batchSize
	cfg.StatementTimeout = timeout
	cfg.SchemaFile = schemaFile

	// Context for pipeline — signal handling is done inside the pipeline
	// (graceful stop at batch boundary on first signal, force exit on second)
	ctx := context.Background()

	pgKit := &calibrate.DialectKit{
		NewDB: func(c calibrate.PipelineConfig) (calibrate.DialectDB, error) {
			return calibrate.NewDB(c)
		},
		DDL: &calibrate.PgDDLGenerator{},
		NewDataPopulator: func(db calibrate.DialectDB, c calibrate.PipelineConfig) calibrate.DataPopulator {
			return calibrate.NewDataGenerator(db, c)
		},
		MapTypes:      func(d calibrate.Domain) calibrate.Domain { return d }, // identity for PG
		ScorerDialect: "postgresql",
	}

	pipeline, err := calibrate.NewPipeline(cfg, pgKit)
	if err != nil {
		log.Fatalf("Failed to initialize pipeline: %v", err)
	}
	defer pipeline.Close()

	switch phase {
	case "init":
		if err := pipeline.Init(ctx); err != nil {
			log.Fatalf("Init failed: %v", err)
		}
		log.Println("Initialization complete.")

	case "generate":
		if err := pipeline.Init(ctx); err != nil {
			log.Fatalf("Init failed: %v", err)
		}
		if err := pipeline.Generate(ctx); err != nil {
			log.Fatalf("Generate failed: %v", err)
		}
		log.Println("Schema and query generation complete.")

	case "run":
		if err := pipeline.Run(ctx); err != nil {
			log.Fatalf("Run failed: %v", err)
		}
		log.Println("EXPLAIN execution complete.")

	case "calculate":
		weights, err := pipeline.Calculate(ctx)
		if err != nil {
			log.Fatalf("Calculate failed: %v", err)
		}
		printWeights(weights)
		if err := calibrate.WriteWeightsJSON(weights, outputFile); err != nil {
			log.Fatalf("Failed to write output: %v", err)
		}
		log.Printf("Weights written to %s", outputFile)

	case "all":
		weights, err := pipeline.All(ctx)
		if err != nil {
			log.Fatalf("Pipeline failed: %v", err)
		}
		printWeights(weights)
		if err := calibrate.WriteWeightsJSON(weights, outputFile); err != nil {
			log.Fatalf("Failed to write output: %v", err)
		}
		log.Printf("Weights written to %s", outputFile)

	default:
		log.Fatalf("Unknown phase: %s (valid: init, generate, run, calculate, all)", phase)
	}
}

func printWeights(cw *calibrate.CalibratedWeights) {
	fmt.Println("\n╔══════════════════════════════════════════════════════╗")
	fmt.Println("║          CALIBRATED SCORING WEIGHTS                  ║")
	fmt.Println("╠══════════════════════════════════════════════════════╣")
	fmt.Printf("║  R² = %.4f   Sample Size = %-10d             ║\n", cw.RSquared, cw.SampleSize)
	fmt.Println("╠══════════════════════════════════════════════════════╣")

	for _, rule := range calibrate.RuleFeatures {
		w := cw.Weights[rule]
		bar := ""
		barLen := int(w)
		if barLen > 20 {
			barLen = 20
		}
		for i := 0; i < barLen; i++ {
			bar += "█"
		}
		fmt.Printf("║  %-22s %6.2f  %-20s ║\n", rule, w, bar)
	}
	fmt.Println("╚══════════════════════════════════════════════════════╝")

	// JSON summary
	data, _ := json.MarshalIndent(cw, "", "  ")
	fmt.Printf("\nJSON:\n%s\n", string(data))
}
