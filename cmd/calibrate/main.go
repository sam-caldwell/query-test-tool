package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/sqlscore/calibrate"
)

func main() {
	var (
		dsn          string
		phase        string
		schemas      int
		queries      int
		rows         int
		workers      int
		timeout      int
		outputFile   string
	)

	flag.StringVar(&dsn, "dsn", "", "PostgreSQL connection string (default: postgres://localhost:5432/sqlscore_calibrate?sslmode=disable)")
	flag.StringVar(&phase, "phase", "all", "Pipeline phase: init, generate, run, calculate, or all")
	flag.IntVar(&schemas, "schemas", 10000, "Target number of schema variants to generate")
	flag.IntVar(&queries, "queries", 1000000, "Target number of queries to generate")
	flag.IntVar(&rows, "rows", 1000, "Rows per table for data generation")
	flag.IntVar(&workers, "workers", 8, "Concurrent EXPLAIN workers")
	flag.IntVar(&timeout, "timeout", 5000, "Per-query statement timeout (ms)")
	flag.StringVar(&outputFile, "output", "calibrated_weights.json", "Output file for calculated weights")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `sqlscore calibrate — Weight calibration via EXPLAIN ANALYZE

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

	cfg := calibrate.DefaultConfig()
	if dsn != "" {
		cfg.DSN = dsn
	}
	cfg.SchemaCount = schemas
	cfg.QueryCount = queries
	cfg.RowsPerTable = rows
	cfg.Workers = workers
	cfg.StatementTimeout = timeout

	// Setup context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Received interrupt, shutting down gracefully...")
		cancel()
	}()

	pipeline, err := calibrate.NewPipeline(cfg)
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
