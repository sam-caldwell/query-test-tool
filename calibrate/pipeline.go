package calibrate

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
)

// Pipeline orchestrates the complete calibration workflow.
type Pipeline struct {
	db  *DB
	cfg PipelineConfig
}

// NewPipeline creates a new calibration pipeline.
func NewPipeline(cfg PipelineConfig) (*Pipeline, error) {
	db, err := NewDB(cfg)
	if err != nil {
		return nil, err
	}
	return &Pipeline{db: db, cfg: cfg}, nil
}

// Close releases pipeline resources.
func (p *Pipeline) Close() error {
	return p.db.Close()
}

// Init creates tracking tables.
func (p *Pipeline) Init(ctx context.Context) error {
	log.Println("Initializing calibration tracking tables...")
	return p.db.InitTrackingTables(ctx)
}

// Generate creates schemas, populates data, and generates queries.
func (p *Pipeline) Generate(ctx context.Context) error {
	start := time.Now()

	// Phase 1: Generate schemas
	log.Printf("Generating %d schema variants across 5 domains...", p.cfg.SchemaCount)
	sg := NewSchemaGenerator(time.Now().UnixNano())
	familyPlans := sg.GenerateAll(p.cfg.SchemaCount)

	totalSchemas := 0
	var familyIDs []int

	for _, plan := range familyPlans {
		familyID, err := p.db.InsertFamily(ctx, plan.Domain.Name, plan.FamilyName, plan.Description)
		if err != nil {
			return fmt.Errorf("inserting family %s: %w", plan.FamilyName, err)
		}
		familyIDs = append(familyIDs, familyID)

		// Insert and apply optimal schema
		if _, err := p.db.InsertSchemaInstance(ctx, familyID, plan.Optimal.SchemaName, true, nil, plan.Optimal.DDL); err != nil {
			return fmt.Errorf("inserting optimal schema: %w", err)
		}
		if err := p.db.ApplySchema(ctx, plan.Optimal.SchemaName, plan.Optimal.DDL); err != nil {
			return fmt.Errorf("applying optimal schema: %w", err)
		}
		totalSchemas++

		// Populate optimal schema with data
		dg := NewDataGenerator(p.db, p.cfg)
		if err := dg.PopulateSchema(ctx, plan.Optimal.SchemaName, plan.Domain); err != nil {
			return fmt.Errorf("populating optimal schema %s: %w", plan.Optimal.SchemaName, err)
		}

		// Insert and apply variants
		for _, variant := range plan.Variants {
			if _, err := p.db.InsertSchemaInstance(ctx, familyID, variant.SchemaName, false, variant.Mutations, variant.DDL); err != nil {
				return fmt.Errorf("inserting variant: %w", err)
			}
			if err := p.db.ApplySchema(ctx, variant.SchemaName, variant.DDL); err != nil {
				log.Printf("Warning: failed to apply schema %s: %v", variant.SchemaName, err)
				continue
			}

			// Populate with same data pattern
			if err := dg.PopulateSchema(ctx, variant.SchemaName, plan.Domain); err != nil {
				log.Printf("Warning: failed to populate %s: %v", variant.SchemaName, err)
			}
			totalSchemas++

			if totalSchemas%100 == 0 {
				log.Printf("  Created %d/%d schemas...", totalSchemas, p.cfg.SchemaCount)
			}
		}
	}
	log.Printf("Created %d schemas in %v", totalSchemas, time.Since(start))

	// Phase 2: Generate queries
	log.Printf("Generating %d queries...", p.cfg.QueryCount)
	queryStart := time.Now()
	qg := NewQueryGenerator(time.Now().UnixNano())

	queriesPerFamily := p.cfg.QueryCount / len(familyPlans)
	totalQueries := 0

	for i, plan := range familyPlans {
		queries := qg.GenerateQueries(plan.Domain, familyIDs[i], queriesPerFamily)

		// Batch insert queries
		batchSize := 1000
		for j := 0; j < len(queries); j += batchSize {
			end := j + batchSize
			if end > len(queries) {
				end = len(queries)
			}
			if err := p.db.InsertQueryBatch(ctx, queries[j:end]); err != nil {
				return fmt.Errorf("inserting query batch: %w", err)
			}
		}
		totalQueries += len(queries)

		if (i+1)%1 == 0 {
			log.Printf("  Generated queries for %d/%d families (%d total)",
				i+1, len(familyPlans), totalQueries)
		}
	}
	log.Printf("Generated %d queries in %v", totalQueries, time.Since(queryStart))

	return nil
}

// Run executes EXPLAIN ANALYZE on all queries.
func (p *Pipeline) Run(ctx context.Context) error {
	log.Printf("Running EXPLAIN ANALYZE with %d workers...", p.cfg.Workers)
	start := time.Now()

	// Load families
	rows, err := p.db.conn.QueryContext(ctx, "SELECT id, domain, name FROM calibration.families")
	if err != nil {
		return err
	}
	defer rows.Close()

	var families []SchemaFamily
	for rows.Next() {
		var f SchemaFamily
		if err := rows.Scan(&f.ID, &f.Domain, &f.Name); err != nil {
			return err
		}
		families = append(families, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	runner := NewRunner(p.db, p.cfg)
	if err := runner.RunAll(ctx, families, ProgressLogger); err != nil {
		return err
	}

	log.Printf("EXPLAIN execution completed in %v", time.Since(start))
	return nil
}

// Calculate performs the regression and outputs weights.
func (p *Pipeline) Calculate(ctx context.Context) (*CalibratedWeights, error) {
	log.Println("Loading calibration results...")
	rows, err := p.db.LoadResultsForRegression(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading results: %w", err)
	}
	log.Printf("Loaded %d regression data points", len(rows))

	if len(rows) == 0 {
		return nil, fmt.Errorf("no calibration results found — run the 'run' phase first")
	}

	log.Println("Calculating weights via OLS regression...")
	weights, err := CalculateWeights(rows)
	if err != nil {
		return nil, err
	}

	// Validate
	issues := ValidateWeights(weights)
	if len(issues) > 0 {
		log.Println("Weight validation warnings:")
		for _, issue := range issues {
			log.Printf("  ⚠ %s", issue)
		}
	}

	return weights, nil
}

// All runs the complete pipeline: init → generate → run → calculate.
func (p *Pipeline) All(ctx context.Context) (*CalibratedWeights, error) {
	if err := p.Init(ctx); err != nil {
		return nil, fmt.Errorf("init: %w", err)
	}
	if err := p.Generate(ctx); err != nil {
		return nil, fmt.Errorf("generate: %w", err)
	}
	if err := p.Run(ctx); err != nil {
		return nil, fmt.Errorf("run: %w", err)
	}
	return p.Calculate(ctx)
}

// WeightsFileFormat is the JSON format expected by scorer/weights.go for embedding.
type WeightsFileFormat struct {
	Version     int            `json:"version"`
	Description string         `json:"description"`
	RSquared    float64        `json:"r_squared"`
	SampleSize  int            `json:"sample_size"`
	GeneratedAt string         `json:"generated_at"`
	Weights     map[string]int `json:"weights"`
}

// WriteWeightsJSON writes calibrated weights to the scorer/weights.json format.
// This file is embedded at build time by cmd/sqlscore via go:embed.
func WriteWeightsJSON(weights *CalibratedWeights, path string) error {
	// Convert float64 weights to int (rounded)
	intWeights := make(map[string]int)
	for rule, w := range weights.Weights {
		if rule == "intercept" {
			continue
		}
		rounded := int(w + 0.5)
		if rounded < 0 {
			rounded = 0
		}
		intWeights[rule] = rounded
	}

	out := WeightsFileFormat{
		Version:     1,
		Description: fmt.Sprintf("Calibrated weights from %d samples (R²=%.4f)", weights.SampleSize, weights.RSquared),
		RSquared:    weights.RSquared,
		SampleSize:  weights.SampleSize,
		GeneratedAt: weights.GeneratedAt.Format("2006-01-02T15:04:05Z"),
		Weights:     intWeights,
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
