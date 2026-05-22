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
	"strings"
	"syscall"

	"github.com/sam-caldwell/query-test-tool/calibrate"
	"github.com/sam-caldwell/query-test-tool/calibrate/mysqldb"
	"github.com/sam-caldwell/query-test-tool/dialect"
	"github.com/sam-caldwell/query-test-tool/scorer"
	mysqlscorer "github.com/sam-caldwell/query-test-tool/scorer/mysql"
)

// enforceSingleInstance uses a PID file lock to ensure only one mysql_calibrate
// instance runs at a time.
func enforceSingleInstance() func() {
	pidFile := os.TempDir() + "/mysql_calibrate.pid"

	if data, err := os.ReadFile(pidFile); err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil && pid != os.Getpid() {
			if process, err := os.FindProcess(pid); err == nil {
				if err := process.Signal(syscall.Signal(0)); err == nil {
					log.Fatalf("FATAL: another mysql_calibrate instance is already running (PID %d). "+
						"Kill the existing instance first, or remove %s if the process is gone.", pid, pidFile)
				}
			}
		}
		os.Remove(pidFile)
	}

	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		log.Fatalf("FATAL: cannot write PID file %s: %v", pidFile, err)
	}

	return func() { os.Remove(pidFile) }
}

func main() {
	cleanupPID := enforceSingleInstance()
	defer cleanupPID()

	// Register MySQL scorer for the runner
	scorer.RegisterDialectScorer(dialect.MySQL, mysqlscorer.ScoreQuery)

	var (
		dsn        string
		dbHost     string
		dbPort     int
		dbUser     string
		dbPassword string
		dbName     string
		phase      string
		schemas    int
		queries    int
		rows       int
		workers    int
		batchSize  int
		timeout    int
		outputFile string
		logFile    string
	)

	flag.StringVar(&dsn, "dsn", "", "Full MySQL connection string (user:password@tcp(host:port)/dbname)")
	flag.StringVar(&dbHost, "host", "localhost", "MySQL host")
	flag.IntVar(&dbPort, "port", 3306, "MySQL port")
	flag.StringVar(&dbUser, "user", "", "MySQL user")
	flag.StringVar(&dbPassword, "password", "", "MySQL password")
	flag.StringVar(&dbName, "dbname", "sqlstore", "MySQL database name")
	flag.StringVar(&phase, "phase", "all", "Pipeline phase: init, generate, run, calculate, or all")
	defaultCfg := calibrate.DefaultConfig()
	flag.IntVar(&schemas, "schemas", defaultCfg.SchemaCount, "Target number of schema variants to generate")
	flag.IntVar(&queries, "queries", defaultCfg.QueryCount, "Target number of queries to generate")
	flag.IntVar(&rows, "rows", defaultCfg.RowsPerTable, "Base rows per table")
	flag.IntVar(&workers, "workers", defaultCfg.Workers, "Concurrent workers")
	flag.IntVar(&batchSize, "batch-size", defaultCfg.BatchSize, "Schemas per batch in batch-and-drop mode (0 to disable)")
	flag.IntVar(&timeout, "timeout", 5000, "Per-query statement timeout (ms)")
	flag.StringVar(&logFile, "logfile", "", "Log file path (use .gz extension for gzip compression)")
	flag.StringVar(&outputFile, "output", "scorer/weights/mysql.json", "Output file for calculated weights")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `mysql_calibrate — MySQL weight calibration via EXPLAIN

Generates calibrated scoring weights for the MySQL dialect by:
  1. Creating schema variants with deliberate anti-patterns
  2. Running EXPLAIN FORMAT=JSON against generated queries
  3. Correlating static scores with actual query costs
  4. Producing a weights file for use by sqlscore --db mysql

Usage:
  mysql_calibrate [flags]

Examples:
  mysql_calibrate -dsn "sqlstore:dbpassword@tcp(192.168.3.159:3306)/sqlstore" -schemas 1000 -queries 100000
  mysql_calibrate -host 192.168.3.159 -user sqlstore -password dbpassword -dbname sqlstore
  mysql_calibrate -phase calculate -output scorer/weights/mysql.json

Flags:
`)
		flag.PrintDefaults()
	}

	flag.Parse()

	// Setup logging
	if logFile != "" {
		var w io.Writer
		f, err := os.Create(logFile)
		if err != nil {
			log.Fatalf("Cannot create log file: %v", err)
		}
		defer f.Close()
		if strings.HasSuffix(logFile, ".gz") {
			gz := gzip.NewWriter(f)
			defer gz.Close()
			w = io.MultiWriter(os.Stderr, gz)
		} else {
			w = io.MultiWriter(os.Stderr, f)
		}
		log.SetOutput(w)
	}

	// Build DSN if not provided directly
	if dsn == "" {
		if dbUser == "" {
			log.Fatal("Either -dsn or -user must be specified")
		}
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", dbUser, dbPassword, dbHost, dbPort, dbName)
	}

	cfg := calibrate.PipelineConfig{
		DSN:              dsn,
		SchemaCount:      schemas,
		QueryCount:       queries,
		RowsPerTable:     rows,
		Workers:          workers,
		BatchSize:        batchSize,
		StatementTimeout: timeout,
	}

	// MySQL DialectKit
	mysqlKit := &calibrate.DialectKit{
		NewDB: func(c calibrate.PipelineConfig) (calibrate.DialectDB, error) {
			return mysqldb.NewDB(c)
		},
		DDL: &mysqldb.MySQLDDLGenerator{},
		NewDataPopulator: func(db calibrate.DialectDB, c calibrate.PipelineConfig) calibrate.DataPopulator {
			return mysqldb.NewMySQLDataPopulator(db, c)
		},
		MapTypes:          mysqldb.MapPgTypesToMySQL,
		NewQueryGenerator: mysqldb.NewMySQLQueryGenerator,
		ScorerDialect:     "mysql",
	}

	ctx := context.Background()

	pipeline, err := calibrate.NewPipeline(cfg, mysqlKit)
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
		log.Println("Generation complete.")

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
		if err := calibrate.WriteWeightsJSON(weights, outputFile); err != nil {
			log.Fatalf("Failed to write weights: %v", err)
		}
		log.Printf("Weights written to %s", outputFile)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(weights)

	case "all":
		weights, err := pipeline.All(ctx)
		if err != nil {
			log.Fatalf("Pipeline failed: %v", err)
		}
		if err := calibrate.WriteWeightsJSON(weights, outputFile); err != nil {
			log.Fatalf("Failed to write weights: %v", err)
		}
		log.Printf("Weights written to %s", outputFile)

	default:
		log.Fatalf("Unknown phase: %s", phase)
	}
}
