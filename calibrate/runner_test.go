package calibrate

import (
	"runtime"
	"sync"
	"testing"

	"github.com/sam-caldwell/query-test-tool/scorer"
)

func TestScoreCacheGetSet(t *testing.T) {
	cache := newScoreCache()

	// Cache miss
	_, ok := cache.get(123)
	if ok {
		t.Error("expected cache miss for unknown key")
	}

	// Store a report
	report := &scorer.Report{
		TotalScore: 42,
	}
	cache.set(123, report)

	// Cache hit
	got, ok := cache.get(123)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.TotalScore != 42 {
		t.Errorf("expected TotalScore=42, got %d", got.TotalScore)
	}
}

func TestScoreCacheConcurrent(t *testing.T) {
	cache := newScoreCache()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			report := &scorer.Report{TotalScore: idx}
			cache.set(uint64(idx), report)
		}(i)
	}
	wg.Wait()

	// Verify all entries
	for i := 0; i < 100; i++ {
		got, ok := cache.get(uint64(i))
		if !ok {
			t.Errorf("expected cache hit for key %d", i)
			continue
		}
		if got.TotalScore != i {
			t.Errorf("key %d: expected TotalScore=%d, got %d", i, i, got.TotalScore)
		}
	}
}

func TestHashSQL(t *testing.T) {
	h1 := hashSQL("SELECT * FROM users")
	h2 := hashSQL("SELECT * FROM users")
	h3 := hashSQL("SELECT * FROM orders")

	if h1 != h2 {
		t.Error("same SQL should produce same hash")
	}
	if h1 == h3 {
		t.Error("different SQL should produce different hash")
	}
}

func TestScoreCacheWithRealScorer(t *testing.T) {
	cache := newScoreCache()
	sql := "SELECT * FROM users WHERE id = 1"
	h := hashSQL(sql)

	// First call: cache miss, score and store
	report, err := scorer.ScoreQuery(sql)
	if err != nil {
		t.Fatalf("ScoreQuery failed: %v", err)
	}
	cache.set(h, report)

	// Second call: cache hit
	cached, ok := cache.get(h)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if cached.TotalScore != report.TotalScore {
		t.Errorf("cached score %d != original %d", cached.TotalScore, report.TotalScore)
	}
}

func TestDefaultConfigWorkers(t *testing.T) {
	cfg := DefaultConfig()
	expected := runtime.NumCPU() * 3
	if expected < 4 {
		expected = 4
	}
	if cfg.Workers != expected {
		t.Errorf("expected default workers=%d (NumCPU=%d × 3), got %d", expected, runtime.NumCPU(), cfg.Workers)
	}
}
