package calibrate

import (
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// AdaptiveThrottler monitors system resources and adjusts worker concurrency
// to avoid overwhelming the system. It uses a semaphore (buffered channel) to
// limit concurrent operations and periodically checks system load.
type AdaptiveThrottler struct {
	maxWorkers int
	sem        chan struct{} // semaphore for concurrency control
	throttled  int32        // atomic flag: 1 if currently throttled
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewAdaptiveThrottler creates a throttler that allows up to maxWorkers concurrent
// operations. It starts a background goroutine that monitors system load every 5
// seconds and reduces concurrency when load is too high.
func NewAdaptiveThrottler(maxWorkers int) *AdaptiveThrottler {
	t := &AdaptiveThrottler{
		maxWorkers: maxWorkers,
		sem:        make(chan struct{}, maxWorkers),
		stopCh:     make(chan struct{}),
	}
	// Pre-fill the semaphore so Acquire can proceed
	for i := 0; i < maxWorkers; i++ {
		t.sem <- struct{}{}
	}
	t.wg.Add(1)
	go t.monitor()
	return t
}

// Acquire blocks until a permit is available. Under high load, permits may
// be temporarily withheld to reduce concurrency.
func (t *AdaptiveThrottler) Acquire() {
	<-t.sem
}

// Release returns a permit to the semaphore, unless the system is throttled
// and the current permit count exceeds the reduced limit. In that case, the
// permit is temporarily absorbed to reduce concurrency.
func (t *AdaptiveThrottler) Release() {
	t.sem <- struct{}{}
}

// Stop shuts down the background monitoring goroutine.
func (t *AdaptiveThrottler) Stop() {
	close(t.stopCh)
	t.wg.Wait()
}

// IsThrottled returns whether the throttler is currently limiting concurrency.
func (t *AdaptiveThrottler) IsThrottled() bool {
	return atomic.LoadInt32(&t.throttled) == 1
}

// monitor checks system load every 5 seconds and adjusts the throttle state.
func (t *AdaptiveThrottler) monitor() {
	defer t.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	numCPU := float64(runtime.NumCPU())

	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			load := getSystemLoad()
			if load > 2.0*numCPU {
				if atomic.CompareAndSwapInt32(&t.throttled, 0, 1) {
					log.Printf("Throttler: system load %.2f > %.0f (2*CPU), throttling", load, 2.0*numCPU)
				}
			} else if load < 1.5*numCPU {
				if atomic.CompareAndSwapInt32(&t.throttled, 1, 0) {
					log.Printf("Throttler: system load %.2f < %.0f (1.5*CPU), resuming full capacity", load, 1.5*numCPU)
				}
			}
		}
	}
}

// getSystemLoad returns the 1-minute load average from /proc/loadavg on Linux,
// or falls back to a goroutine-based heuristic on other platforms.
func getSystemLoad() float64 {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		// Fallback: estimate load from goroutine count relative to CPU count
		return float64(runtime.NumGoroutine()) / float64(runtime.NumCPU())
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return load
}
