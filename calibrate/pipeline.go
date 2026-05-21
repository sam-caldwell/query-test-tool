package calibrate

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Pipeline orchestrates the complete calibration workflow.
type Pipeline struct {
	db       DialectDB
	kit      *DialectKit
	cfg      PipelineConfig
	stopOnce sync.Once
	stopCh   chan struct{} // closed when graceful stop is requested
}

// NewPipeline creates a new calibration pipeline using the provided dialect kit.
func NewPipeline(cfg PipelineConfig, kit *DialectKit) (*Pipeline, error) {
	db, err := kit.NewDB(cfg)
	if err != nil {
		return nil, err
	}
	p := &Pipeline{db: db, kit: kit, cfg: cfg, stopCh: make(chan struct{})}
	p.installSignalHandler()
	return p, nil
}

// NewPipelineWithDB creates a pipeline with a pre-existing DB connection (for testing).
func NewPipelineWithDB(cfg PipelineConfig, kit *DialectKit, db DialectDB) *Pipeline {
	p := &Pipeline{db: db, kit: kit, cfg: cfg, stopCh: make(chan struct{})}
	p.installSignalHandler()
	return p
}

// installSignalHandler listens for SIGINT/SIGTERM and requests a graceful stop
// at the next batch boundary. A second signal forces immediate exit.
func (p *Pipeline) installSignalHandler() {
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received %v — will stop after current batch completes (send again to force quit)", sig)
		p.stopOnce.Do(func() { close(p.stopCh) })
		// Second signal = hard exit
		sig = <-sigCh
		log.Printf("Received %v again — forcing immediate exit", sig)
		os.Exit(1)
	}()
}

// shouldStop returns true if a graceful stop has been requested.
func (p *Pipeline) shouldStop() bool {
	select {
	case <-p.stopCh:
		return true
	default:
		return false
	}
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

	// Phase 1: Generate schemas (parallelized)
	log.Printf("Generating %d schema variants across %d domains (%d workers)...", p.cfg.SchemaCount, len(Archetypes()), p.cfg.Workers)
	sg := NewSchemaGenerator(time.Now().UnixNano())
	familyPlans := sg.GenerateAll(p.cfg.SchemaCount)

	// If a custom schema file is provided, import it and add as an additional family
	if p.cfg.SchemaFile != "" {
		importedDomain, err := ImportSchemaFile(p.cfg.SchemaFile)
		if err != nil {
			return fmt.Errorf("importing schema file %s: %w", p.cfg.SchemaFile, err)
		}
		log.Printf("Imported custom schema %q from %s (%d tables, %d indexes, %d foreign keys)",
			importedDomain.Name, p.cfg.SchemaFile,
			len(importedDomain.Tables), len(importedDomain.Indexes), len(importedDomain.ForeignKeys))

		// Generate variants for the imported domain using the same mutation pipeline
		variantsPerFamily := p.cfg.SchemaCount / len(Archetypes())
		variants := GenerateSchemaVariants(*importedDomain, variantsPerFamily-1, sg.rng)

		plan := SchemaFamilyPlan{
			Domain:      *importedDomain,
			FamilyName:  fmt.Sprintf("%s_family", importedDomain.Name),
			Description: importedDomain.Description,
		}

		schemaName := fmt.Sprintf("cal_imp_%05d", 0)
		plan.Optimal = SchemaInstance{
			SchemaName: schemaName,
			IsOptimal:  true,
			DDL:        p.kit.DDL.GenerateDDL(*importedDomain, schemaName),
		}

		counter := 1
		for _, mutationSet := range variants {
			degraded := applyMutations(*importedDomain, mutationSet)
			sn := fmt.Sprintf("cal_imp_%05d", counter)
			ddl := p.kit.DDL.GenerateDDL(degraded, sn)

			var mutNames []string
			for _, m := range mutationSet {
				mutNames = append(mutNames, m.Name)
			}

			plan.Variants = append(plan.Variants, SchemaInstance{
				SchemaName: sn,
				IsOptimal:  false,
				Mutations:  mutNames,
				DDL:        ddl,
			})
			counter++
		}

		familyPlans = append(familyPlans, plan)
	}

	var totalSchemas int64
	familyIDs := make([]int, len(familyPlans))

	// First, register families (sequential — needs IDs for later steps)
	for i, plan := range familyPlans {
		familyID, err := p.db.InsertFamily(ctx, plan.Domain.Name, plan.FamilyName, plan.Description)
		if err != nil {
			return fmt.Errorf("inserting family %s: %w", plan.FamilyName, err)
		}
		familyIDs[i] = familyID
	}

	// Apply optimal schemas concurrently across all families
	{
		var optWg sync.WaitGroup
		optErrCh := make(chan error, len(familyPlans))
		optSem := make(chan struct{}, p.cfg.Workers)
		for i, plan := range familyPlans {
			optWg.Add(1)
			go func(idx int, pl SchemaFamilyPlan) {
				defer optWg.Done()
				optSem <- struct{}{}
				defer func() { <-optSem }()

				if _, err := p.db.InsertSchemaInstance(ctx, familyIDs[idx], pl.Optimal.SchemaName, true, nil, pl.Optimal.DDL); err != nil {
					optErrCh <- fmt.Errorf("inserting optimal schema: %w", err)
					return
				}
				if err := p.db.ApplySchema(ctx, pl.Optimal.SchemaName, pl.Optimal.DDL); err != nil {
					optErrCh <- fmt.Errorf("applying optimal schema: %w", err)
					return
				}
				dg := p.kit.NewDataPopulator(p.db, p.cfg)
				if err := dg.PopulateSchema(ctx, pl.Optimal.SchemaName, pl.Domain); err != nil {
					optErrCh <- fmt.Errorf("populating optimal schema %s: %w", pl.Optimal.SchemaName, err)
					return
				}
				atomic.AddInt64(&totalSchemas, 1)
			}(i, plan)
		}
		optWg.Wait()
		close(optErrCh)
		for err := range optErrCh {
			if err != nil {
				return err
			}
		}
	}

	// Now create variants concurrently
	type schemaJob struct {
		familyID int
		variant  SchemaInstance
		domain   Domain
	}

	jobCh := make(chan schemaJob, p.cfg.Workers*2)
	var wg sync.WaitGroup

	for i := 0; i < p.cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dg := p.kit.NewDataPopulator(p.db, p.cfg)
			for job := range jobCh {
				if _, err := p.db.InsertSchemaInstance(ctx, job.familyID, job.variant.SchemaName, false, job.variant.Mutations, job.variant.DDL); err != nil {
					log.Printf("Warning: failed to insert schema %s: %v", job.variant.SchemaName, err)
					continue
				}
				if err := p.db.ApplySchema(ctx, job.variant.SchemaName, job.variant.DDL); err != nil {
					log.Printf("Warning: failed to apply schema %s: %v", job.variant.SchemaName, err)
					continue
				}
				if err := dg.PopulateSchema(ctx, job.variant.SchemaName, job.domain); err != nil {
					log.Printf("Warning: failed to populate %s: %v", job.variant.SchemaName, err)
				}
				n := atomic.AddInt64(&totalSchemas, 1)
				if n%100 == 0 {
					log.Printf("  Created %d/%d schemas...", n, p.cfg.SchemaCount)
				}
			}
		}()
	}

	for i, plan := range familyPlans {
		for _, variant := range plan.Variants {
			select {
			case jobCh <- schemaJob{familyID: familyIDs[i], variant: variant, domain: plan.Domain}:
			case <-ctx.Done():
				close(jobCh)
				wg.Wait()
				return ctx.Err()
			}
		}
	}
	close(jobCh)
	wg.Wait()

	log.Printf("Created %d schemas in %v", atomic.LoadInt64(&totalSchemas), time.Since(start))

	// Phase 2: Generate queries (producer-consumer: overlap CPU gen with DB insertion)
	log.Printf("Generating %d queries...", p.cfg.QueryCount)
	queryStart := time.Now()

	queriesPerFamily := p.cfg.QueryCount / len(familyPlans)
	var totalQueries int64

	type queryBatch struct {
		queries []GeneratedQuery
	}
	batchCh := make(chan queryBatch, len(familyPlans)*2)

	// Consumer: batch-inserts queries as they arrive
	var consumerWg sync.WaitGroup
	var insertErr error
	consumerWg.Add(1)
	go func() {
		defer consumerWg.Done()
		for batch := range batchCh {
			batchSize := 1000
			for j := 0; j < len(batch.queries); j += batchSize {
				end := j + batchSize
				if end > len(batch.queries) {
					end = len(batch.queries)
				}
				if err := p.db.InsertQueryBatch(ctx, batch.queries[j:end]); err != nil {
					insertErr = fmt.Errorf("inserting query batch: %w", err)
					return
				}
			}
			n := atomic.AddInt64(&totalQueries, int64(len(batch.queries)))
			log.Printf("  Inserted %d queries so far...", n)
		}
	}()

	// Producers: generate queries for all families concurrently
	var genWg sync.WaitGroup
	for i, plan := range familyPlans {
		genWg.Add(1)
		go func(idx int, pl SchemaFamilyPlan) {
			defer genWg.Done()
			qg := NewQueryGenerator(time.Now().UnixNano() + int64(idx))
			queries := qg.GenerateQueries(pl.Domain, familyIDs[idx], queriesPerFamily)
			batchCh <- queryBatch{queries: queries}
		}(i, plan)
	}
	genWg.Wait()
	close(batchCh)
	consumerWg.Wait()

	if insertErr != nil {
		return insertErr
	}
	log.Printf("Generated %d queries in %v", atomic.LoadInt64(&totalQueries), time.Since(queryStart))

	return nil
}

// Run executes EXPLAIN ANALYZE on all queries.
func (p *Pipeline) Run(ctx context.Context) error {
	log.Printf("Running EXPLAIN ANALYZE with %d workers...", p.cfg.Workers)
	start := time.Now()

	// Load families
	rows, err := p.db.Conn().QueryContext(ctx, "SELECT id, domain, name FROM calibration.families")
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

	runner := NewRunner(p.db, p.cfg, p.kit.ScorerDialect)
	throttler := NewAdaptiveThrottler(p.cfg.Workers)
	defer throttler.Stop()
	runner.SetThrottler(throttler)
	if err := runner.RunAll(ctx, families, 0, nil, ProgressLogger); err != nil {
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

	log.Println("Calculating weights via paired comparison...")
	weights, err := CalculateWeightsDirect(rows)
	if err != nil {
		// Fall back to OLS if direct method fails
		log.Printf("Direct method failed (%v), falling back to OLS regression...", err)
		weights, err = CalculateWeights(rows)
		if err != nil {
			return nil, err
		}
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

// All runs the complete pipeline. When BatchSize > 0, uses batch-and-drop mode
// to limit peak disk usage: creates a batch of schemas, runs EXPLAIN ANALYZE,
// stores results, then drops the schemas before creating the next batch.
func (p *Pipeline) All(ctx context.Context) (*CalibratedWeights, error) {
	if p.cfg.BatchSize > 0 {
		return p.AllBatched(ctx)
	}
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

// AllBatched runs the calibration pipeline in batch-and-drop mode.
// Fully reentrant — the DB is the source of truth:
//   - Checks existing families, schema instances, queries, and results
//   - Only generates what's missing
//   - Resumes from the exact schema where a prior run was interrupted
//   - Random seed is NOT fixed — new schemas on each run, prior schemas preserved
func (p *Pipeline) AllBatched(ctx context.Context) (*CalibratedWeights, error) {
	if err := p.Init(ctx); err != nil {
		return nil, fmt.Errorf("init: %w", err)
	}

	start := time.Now()
	batchSize := p.cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 10
	}

	// Pre-create result partitions (one per batch + legacy partition 0)
	totalBatches := (p.cfg.SchemaCount + batchSize - 1) / batchSize
	if err := p.db.CreateResultPartitions(ctx, totalBatches); err != nil {
		return nil, fmt.Errorf("creating result partitions: %w", err)
	}

	// Clean up orphan data schemas from prior interrupted runs
	orphans, err := p.db.DropOrphanSchemas(ctx)
	if err != nil {
		log.Printf("Warning: orphan cleanup failed: %v", err)
	} else if orphans > 0 {
		log.Printf("Cleaned up %d orphan schemas from prior run", orphans)
	}

	// Check existing state
	existingResults, _ := p.db.CountTotalResults(ctx)
	if existingResults > 0 {
		log.Printf("Resuming: found %d existing results from prior run", existingResults)
	}

	// Phase 1: Ensure all families are registered
	sg := NewSchemaGenerator(time.Now().UnixNano())
	familyPlans := sg.GenerateAll(p.cfg.SchemaCount)

	existingFamilies, err := p.db.GetExistingFamilies(ctx)
	if err != nil {
		// Table might be empty, that's fine
		existingFamilies = make(map[string]int)
	}

	var familyIDs []int
	var newFamilies, reusedFamilies int
	for _, plan := range familyPlans {
		if existingID, ok := existingFamilies[plan.FamilyName]; ok {
			familyIDs = append(familyIDs, existingID)
			reusedFamilies++
		} else {
			familyID, err := p.db.InsertFamily(ctx, plan.Domain.Name, plan.FamilyName, plan.Description)
			if err != nil {
				return nil, fmt.Errorf("inserting family %s: %w", plan.FamilyName, err)
			}
			familyIDs = append(familyIDs, familyID)
			newFamilies++
		}
	}
	if reusedFamilies > 0 {
		log.Printf("Families: %d reused, %d new", reusedFamilies, newFamilies)
	}

	// Phase 2: Register all schema instances (idempotent — ON CONFLICT)
	type schemaWork struct {
		familyIdx int
		instance  SchemaInstance
		domain    Domain
		isOptimal bool
	}
	var allWork []schemaWork
	var newSchemas, existingSchemas int
	for i, plan := range familyPlans {
		// Check how many instances this family already has
		existingCount, _ := p.db.CountSchemaInstancesForFamily(ctx, familyIDs[i])

		// Register optimal
		if _, err := p.db.InsertSchemaInstance(ctx, familyIDs[i], plan.Optimal.SchemaName, true, plan.Optimal.Mutations, plan.Optimal.DDL); err != nil {
			log.Printf("Warning: failed to register schema %s: %v", plan.Optimal.SchemaName, err)
		}
		allWork = append(allWork, schemaWork{
			familyIdx: i,
			instance:  plan.Optimal,
			domain:    plan.Domain,
			isOptimal: true,
		})

		// Register variants
		for _, v := range plan.Variants {
			if _, err := p.db.InsertSchemaInstance(ctx, familyIDs[i], v.SchemaName, false, v.Mutations, v.DDL); err != nil {
				log.Printf("Warning: failed to register schema %s: %v", v.SchemaName, err)
			}
			allWork = append(allWork, schemaWork{
				familyIdx: i,
				instance:  v,
				domain:    plan.Domain,
				isOptimal: false,
			})
		}

		expectedCount := 1 + len(plan.Variants) // optimal + variants
		if existingCount >= expectedCount {
			existingSchemas += existingCount
		} else {
			newSchemas += expectedCount
		}
	}
	log.Printf("Schema instances: %d registered (%d new, %d existing)",
		len(allWork), newSchemas, existingSchemas)

	// Phase 3: Generate queries for families that need them
	queriesPerFamily := p.cfg.QueryCount / len(familyPlans)
	var queriesGenerated, queriesReused int
	for i, plan := range familyPlans {
		existing, err := p.db.CountQueriesForFamily(ctx, familyIDs[i])
		if err != nil {
			return nil, fmt.Errorf("checking queries for family %d: %w", familyIDs[i], err)
		}
		if existing >= queriesPerFamily {
			queriesReused += existing
			continue
		}
		log.Printf("Generating %d queries for family %s...", queriesPerFamily, plan.FamilyName)
		qg := NewQueryGenerator(time.Now().UnixNano() + int64(i))
		queries := qg.GenerateQueries(plan.Domain, familyIDs[i], queriesPerFamily)
		insertBatchSize := 1000
		for j := 0; j < len(queries); j += insertBatchSize {
			end := j + insertBatchSize
			if end > len(queries) {
				end = len(queries)
			}
			if err := p.db.InsertQueryBatch(ctx, queries[j:end]); err != nil {
				return nil, fmt.Errorf("inserting query batch: %w", err)
			}
		}
		queriesGenerated += len(queries)
	}
	if queriesReused > 0 {
		log.Printf("Queries: %d new, %d reused from prior run", queriesGenerated, queriesReused)
	} else if queriesGenerated > 0 {
		log.Printf("Query generation complete: %d queries", queriesGenerated)
	}

	// Phase 4: Find schemas that still need EXPLAIN results.
	// Use DB-sourced family info (not regenerated plans) to ensure correct
	// family assignments on reentrant runs with different random seeds.
	pendingSchemasWithFamily, err := p.db.GetPendingSchemasWithFamily(ctx)
	if err != nil {
		return nil, fmt.Errorf("checking pending schemas: %w", err)
	}

	totalSchemas := len(allWork)
	completedSchemas := totalSchemas - len(pendingSchemasWithFamily)
	if completedSchemas > 0 {
		log.Printf("Progress: %d/%d schemas already have results, %d remaining",
			completedSchemas, totalSchemas, len(pendingSchemasWithFamily))
	}

	if len(pendingSchemasWithFamily) == 0 {
		log.Printf("All schemas have results — skipping to weight calculation")
		return p.Calculate(ctx)
	}

	// Build pending schema names list and a lookup from name to DB-sourced family info
	pendingSchemas := make([]string, len(pendingSchemasWithFamily))
	pendingFamilyByName := make(map[string]PendingSchema)
	for i, ps := range pendingSchemasWithFamily {
		pendingSchemas[i] = ps.SchemaName
		pendingFamilyByName[ps.SchemaName] = ps
	}

	// Build a lookup from domain name to archetype Domain.
	// This is critical for reentrant runs: the schema generator uses a random seed
	// so regenerated plans may assign schema names to different families. We must
	// use the DB-sourced domain name to find the correct archetype for table creation.
	archetypeByDomain := make(map[string]Domain)
	for _, arch := range Archetypes() {
		archetypeByDomain[arch.Name] = arch
	}

	// Build a lookup from schema name to work item for DDL/domain info.
	// For reentrant runs, override the domain with the DB-sourced archetype.
	workByName := make(map[string]schemaWork)
	for _, w := range allWork {
		workByName[w.instance.SchemaName] = w
	}
	// Override domain for pending schemas using DB-sourced family info
	for name, ps := range pendingFamilyByName {
		if w, ok := workByName[name]; ok {
			if arch, ok := archetypeByDomain[ps.Domain]; ok {
				w.domain = arch
				workByName[name] = w
			}
		}
	}

	// Process pending schemas in batches
	runner := NewRunner(p.db, p.cfg, p.kit.ScorerDialect)
	throttler := NewAdaptiveThrottler(p.cfg.Workers)
	defer throttler.Stop()
	runner.SetThrottler(throttler)

	pendingBatches := (len(pendingSchemas) + batchSize - 1) / batchSize
	log.Printf("Batch-and-drop: %d pending schemas, batch size %d, %d batches",
		len(pendingSchemas), batchSize, pendingBatches)

	var totalProcessed int64
	for batchStart := 0; batchStart < len(pendingSchemas); batchStart += batchSize {
		// Check for graceful stop between batches
		if p.shouldStop() {
			log.Printf("Graceful stop requested — stopping after %d schemas processed (%d results persisted)",
				atomic.LoadInt64(&totalProcessed), completedSchemas+int(atomic.LoadInt64(&totalProcessed)))
			break
		}

		batchEnd := batchStart + batchSize
		if batchEnd > len(pendingSchemas) {
			batchEnd = len(pendingSchemas)
		}
		batchNames := pendingSchemas[batchStart:batchEnd]
		batchNum := batchStart/batchSize + 1

		log.Printf("=== Batch %d/%d: %d schemas ===", batchNum, totalBatches, len(batchNames))

		// 1. Create UNLOGGED tables, populate data, then add indexes (concurrently)
		// Split DDL: tables-only first (no indexes/FKs) for fast bulk inserts,
		// then add indexes after data is loaded.
		var createdNames []string
		var schemaMu sync.Mutex
		var batchWg sync.WaitGroup
		for _, name := range batchNames {
			w, ok := workByName[name]
			if !ok {
				log.Printf("Warning: no work item for schema %s, skipping", name)
				continue
			}
			batchWg.Add(1)
			go func(w schemaWork) {
				defer batchWg.Done()
				// Phase 1: Create UNLOGGED tables (no indexes, no FKs)
				tablesDDL := p.kit.DDL.GenerateDDLTablesOnly(w.domain, w.instance.SchemaName)
				if err := p.db.ApplySchema(ctx, w.instance.SchemaName, tablesDDL); err != nil {
					log.Printf("Warning: failed to create tables for %s: %v", w.instance.SchemaName, err)
					return
				}
				// Phase 2: Bulk insert data (no index maintenance overhead)
				dg := p.kit.NewDataPopulator(p.db, p.cfg)
				if err := dg.PopulateSchema(ctx, w.instance.SchemaName, w.domain); err != nil {
					log.Printf("Warning: failed to populate %s: %v", w.instance.SchemaName, err)
					return
				}
				// Phase 3: Add indexes and FKs (builds indexes on existing data — faster than incremental)
				indexDDL := p.kit.DDL.GenerateDDLIndexesAndFKs(w.domain, w.instance.SchemaName)
				if err := p.db.ApplyIndexesAndFKs(ctx, indexDDL); err != nil {
					log.Printf("Warning: failed to create indexes for %s: %v", w.instance.SchemaName, err)
					return
				}
				schemaMu.Lock()
				createdNames = append(createdNames, w.instance.SchemaName)
				schemaMu.Unlock()
			}(w)
		}
		batchWg.Wait()
		log.Printf("  Created %d schemas", len(createdNames))

		// 2. Run EXPLAIN ANALYZE for schemas in this batch.
		// Use DB-sourced family info to ensure correct family assignment.
		var batchFamilies []SchemaFamily
		familySeen := make(map[int]bool)
		for _, name := range batchNames {
			ps, ok := pendingFamilyByName[name]
			if !ok {
				continue
			}
			if !familySeen[ps.FamilyID] {
				familySeen[ps.FamilyID] = true
				batchFamilies = append(batchFamilies, SchemaFamily{
					ID:     ps.FamilyID,
					Domain: ps.Domain,
					Name:   ps.FamilyName,
				})
			}
		}

		// Build filter of only this batch's schema names
		batchFilter := make(map[string]bool)
		for _, name := range createdNames {
			batchFilter[name] = true
		}

		if err := runner.RunAll(ctx, batchFamilies, batchNum, batchFilter, func(done, total int64) {
			if done%1000 == 0 {
				log.Printf("  Batch %d EXPLAIN progress: %d/%d", batchNum, done, total)
			}
		}); err != nil {
			log.Printf("Warning: EXPLAIN batch %d had errors: %v", batchNum, err)
		}

		// 3. Drop ALL data schemas to free disk (not just createdNames — catches orphans too)
		dropped, err := p.db.DropOrphanSchemas(ctx)
		if err != nil {
			log.Printf("Warning: failed to drop schemas: %v", err)
		}
		log.Printf("  Dropped %d schemas to free disk", dropped)

		atomic.AddInt64(&totalProcessed, int64(len(createdNames)))
	}

	log.Printf("Batch processing complete: %d new + %d prior = %d total schemas, %v elapsed",
		atomic.LoadInt64(&totalProcessed), completedSchemas, totalSchemas, time.Since(start))

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
