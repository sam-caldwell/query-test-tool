package calibrate

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"sync"
	"sync/atomic"

	"github.com/sam-caldwell/query-test-tool/src/dialect"
	"github.com/sam-caldwell/query-test-tool/src/scorer"
)

// scoreCache caches scorer results by SQL hash to avoid redundant scoring.
type scoreCache struct {
	mu    sync.RWMutex
	cache map[uint64]*scorer.Report
}

func newScoreCache() *scoreCache {
	return &scoreCache{cache: make(map[uint64]*scorer.Report)}
}

func (sc *scoreCache) get(sqlHash uint64) (*scorer.Report, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	r, ok := sc.cache[sqlHash]
	return r, ok
}

func (sc *scoreCache) set(sqlHash uint64, r *scorer.Report) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.cache[sqlHash] = r
}

func hashSQL(sql string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(sql))
	return h.Sum64()
}

// Runner executes EXPLAIN ANALYZE on queries across schema instances.
type Runner struct {
	db            DialectDB
	cfg           PipelineConfig
	scorerDialect string
	scores        *scoreCache
	throttler     *AdaptiveThrottler
	failCount     int64
	batchID       int
}

// NewRunner creates a new query runner.
func NewRunner(db DialectDB, cfg PipelineConfig, scorerDialect string) *Runner {
	return &Runner{
		db:            db,
		cfg:           cfg,
		scorerDialect: scorerDialect,
		scores:        newScoreCache(),
	}
}

// SetThrottler attaches an adaptive throttler to the runner.
func (r *Runner) SetThrottler(t *AdaptiveThrottler) {
	r.throttler = t
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
// If schemaFilter is non-nil, only schemas whose names are in the filter are used.
func (r *Runner) RunAll(ctx context.Context, families []SchemaFamily, batchID int, schemaFilter map[string]bool, progress func(done, total int64)) error {
	r.batchID = batchID
	// Build work items
	var jobs []RunJob
	for _, fam := range families {
		schemas, err := r.db.GetFamilySchemas(ctx, fam.ID)
		if err != nil {
			return fmt.Errorf("loading schemas for family %d: %w", fam.ID, err)
		}

		// Filter to only schemas in this batch
		if schemaFilter != nil {
			var filtered []SchemaInstance
			for _, s := range schemas {
				if schemaFilter[s.SchemaName] {
					filtered = append(filtered, s)
				}
			}
			schemas = filtered
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

	if total == 0 {
		log.Printf("  WARNING: 0 EXPLAIN jobs generated (families=%d, schemaFilter size=%d)", len(families), len(schemaFilter))
		for _, fam := range families {
			schemas, _ := r.db.GetFamilySchemas(ctx, fam.ID)
			var filtered int
			for _, s := range schemas {
				if schemaFilter != nil && schemaFilter[s.SchemaName] {
					filtered++
				}
			}
			queries, _ := r.db.GetQueriesForFamily(ctx, fam.ID)
			log.Printf("    Family %d (%s): %d total schemas, %d after filter, %d queries",
				fam.ID, fam.Name, len(schemas), filtered, len(queries))
		}
		return nil
	}

	// Execute concurrently
	jobCh := make(chan RunJob, r.cfg.Workers*2)
	var wg sync.WaitGroup

	for i := 0; i < r.cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				if r.throttler != nil {
					r.throttler.Acquire()
				}
				if err := r.executeJob(ctx, job); err != nil {
					// Silently count failures — per-query logging causes multi-GB log files
					atomic.AddInt64(&r.failCount, 1)
				}
				if r.throttler != nil {
					r.throttler.Release()
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

	fails := atomic.LoadInt64(&r.failCount)
	if fails > 0 {
		log.Printf("  EXPLAIN: %d/%d succeeded, %d failed", total-fails, total, fails)
		r.failCount = 0
	}

	return nil
}

// executeJob runs a single EXPLAIN ANALYZE and stores the result.
func (r *Runner) executeJob(ctx context.Context, job RunJob) error {
	explainResult, err := r.db.RunExplain(ctx, job.SchemaInstance.SchemaName, job.Query.SQL)
	if err != nil {
		return err
	}

	// Score the query with query-test-tool, using cache since score depends only on SQL text
	sqlHash := hashSQL(job.Query.SQL)
	report, ok := r.scores.get(sqlHash)
	if !ok {
		var scoreErr error
		report, scoreErr = scorer.ScoreQueryWithDialect(job.Query.SQL, dialect.Dialect(r.scorerDialect))
		if scoreErr != nil {
			return fmt.Errorf("scoring query: %w", scoreErr)
		}
		r.scores.set(sqlHash, report)
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

	return r.db.InsertResult(ctx, scored, r.batchID)
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
