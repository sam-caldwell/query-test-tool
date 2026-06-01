package calibrate

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewAdaptiveThrottler(t *testing.T) {
	throttler := NewAdaptiveThrottler(4)
	defer throttler.Stop()

	if throttler.maxWorkers != 4 {
		t.Errorf("expected maxWorkers=4, got %d", throttler.maxWorkers)
	}
	if throttler.IsThrottled() {
		t.Error("expected throttler to not be throttled initially")
	}
}

func TestAdaptiveThrottlerAcquireRelease(t *testing.T) {
	throttler := NewAdaptiveThrottler(3)
	defer throttler.Stop()

	// Acquire all 3 permits
	throttler.Acquire()
	throttler.Acquire()
	throttler.Acquire()

	// Release should allow re-acquisition
	throttler.Release()
	throttler.Acquire()

	// Clean up
	throttler.Release()
	throttler.Release()
	throttler.Release()
}

func TestAdaptiveThrottlerConcurrency(t *testing.T) {
	maxConcurrent := 5
	throttler := NewAdaptiveThrottler(maxConcurrent)
	defer throttler.Stop()

	var active int64
	var maxSeen int64
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			throttler.Acquire()
			cur := atomic.AddInt64(&active, 1)
			for {
				old := atomic.LoadInt64(&maxSeen)
				if cur <= old || atomic.CompareAndSwapInt64(&maxSeen, old, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt64(&active, -1)
			throttler.Release()
		}()
	}

	wg.Wait()

	if atomic.LoadInt64(&maxSeen) > int64(maxConcurrent) {
		t.Errorf("max concurrent workers %d exceeded limit %d", atomic.LoadInt64(&maxSeen), maxConcurrent)
	}
}

func TestAdaptiveThrottlerStop(t *testing.T) {
	throttler := NewAdaptiveThrottler(2)
	throttler.Stop()
	// Should not panic or deadlock
}

func TestGetSystemLoad(t *testing.T) {
	load := getSystemLoad()
	if load < 0 {
		t.Errorf("expected non-negative load, got %f", load)
	}
	// Just verify it returns a reasonable value without error
}
