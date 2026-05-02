package calibrate

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/sqlscore/scorer"
)

// Runner executes EXPLAIN ANALYZE on queries across schema instances.
type Runner struct {
	db  *DB
	cfg PipelineConfig
}

// NewRunner creates a new query runner.
func NewRunner(db *DB, cfg PipelineConfig) *Runner {
	return &Runner{db: db, cfg: cfg}
}

// RunJob represents a single EXPLAIN job.
type RunJob struct {
	Query          GeneratedQuery
	SchemaInstance SchemaInstance
}

// maxDegradedPerQuery limits how many degraded schemas each query runs against.
// Running every query against every schema is O(queries × schemas) which is too large.
// Instead we run each query against the optimal + a sample of degraded schemas.
const maxDegradedPerQuery = 5

// RunAll executes all queries against their family's schema instances.
func (r *Runner) RunAll(ctx context.Context, families []SchemaFamily, progress func(done, total int64)) error {
	// Build work items
	var jobs []RunJob
	for _, fam := range families {
		schemas, err := r.db.GetFamilySchemas(ctx, fam.ID)
		if err != nil {
			return fmt.Errorf("loading schemas for family %d: %w", fam.ID, err)
		}

		// Separate optimal from degraded
		var optimal []SchemaInstance
		var degraded []SchemaInstance
		for _, s := range schemas {
			if s.IsOptimal {
				optimal = append(optimal, s)
			} else {
				degraded = append(degraded, s)
			}
		}

		queries, err := r.db.GetQueriesForFamily(ctx, fam.ID)
		if err != nil {
			return fmt.Errorf("loading queries for family %d: %w", fam.ID, err)
		}

		for qi, q := range queries {
			// Always run against optimal
			for _, s := range optimal {
				jobs = append(jobs, RunJob{Query: q, SchemaInstance: s})
			}
			// Run against a rotating sample of degraded schemas
			if len(degraded) > 0 {
				sampleSize := maxDegradedPerQuery
				if sampleSize > len(degraded) {
					sampleSize = len(degraded)
				}
				start := (qi * sampleSize) % len(degraded)
				for i := 0; i < sampleSize; i++ {
					idx := (start + i) % len(degraded)
					jobs = append(jobs, RunJob{Query: q, SchemaInstance: degraded[idx]})
				}
			}
		}
	}

	total := int64(len(jobs))
	var done int64

	// Execute concurrently
	jobCh := make(chan RunJob, r.cfg.Workers*2)
	var wg sync.WaitGroup

	for i := 0; i < r.cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				if err := r.executeJob(ctx, job); err != nil {
					log.Printf("EXPLAIN failed (schema=%s, query_type=%s): %v",
						job.SchemaInstance.SchemaName, job.Query.QueryType, err)
				}
				current := atomic.AddInt64(&done, 1)
				if progress != nil && current%1000 == 0 {
					progress(current, total)
				}
			}
		}()
	}

	for _, job := range jobs {
		select {
		case jobCh <- job:
		case <-ctx.Done():
			close(jobCh)
			return ctx.Err()
		}
	}
	close(jobCh)
	wg.Wait()

	return nil
}

// executeJob runs a single EXPLAIN ANALYZE and stores the result.
func (r *Runner) executeJob(ctx context.Context, job RunJob) error {
	explainResult, err := r.db.RunExplain(ctx, job.SchemaInstance.SchemaName, job.Query.SQL)
	if err != nil {
		return err
	}

	// Score the query with sqlscore
	report, err := scorer.ScoreQuery(job.Query.SQL)
	if err != nil {
		return fmt.Errorf("scoring query: %w", err)
	}

	var findings []string
	for _, f := range report.Efficiency.Findings {
		findings = append(findings, f.Rule)
	}
	for _, f := range report.MemoryCompute.Findings {
		findings = append(findings, f.Rule)
	}
	for _, f := range report.CognitiveComplex.Findings {
		findings = append(findings, f.Rule)
	}

	scored := &ScoredResult{
		ExplainResult:   *explainResult,
		ScoreTotal:      report.TotalScore,
		ScoreEfficiency: report.Efficiency.Score,
		ScoreMemory:     report.MemoryCompute.Score,
		ScoreCognitive:  report.CognitiveComplex.Score,
		Findings:        findings,
	}
	scored.QueryID = job.Query.ID
	scored.SchemaInstanceID = job.SchemaInstance.ID

	return r.db.InsertResult(ctx, scored)
}

// RunFamilyBatch executes EXPLAIN for all queries in a single family.
// This is more memory-efficient for large query counts.
func (r *Runner) RunFamilyBatch(ctx context.Context, familyID int) (int, error) {
	schemas, err := r.db.GetFamilySchemas(ctx, familyID)
	if err != nil {
		return 0, err
	}

	queries, err := r.db.GetQueriesForFamily(ctx, familyID)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, q := range queries {
		for _, s := range schemas {
			if err := r.executeJob(ctx, RunJob{Query: q, SchemaInstance: s}); err != nil {
				log.Printf("EXPLAIN failed: %v", err)
				continue
			}
			count++
		}
	}
	return count, nil
}

// ProgressLogger is a simple progress callback.
func ProgressLogger(done, total int64) {
	pct := float64(done) / float64(total) * 100
	log.Printf("Progress: %d/%d (%.1f%%)", done, total, pct)
}
