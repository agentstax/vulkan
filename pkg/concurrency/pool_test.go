package concurrency

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestNewWorkerPoolLimiter_ValidatesLimit(t *testing.T) {
	if _, err := NewWorkerPoolLimiter(0); err == nil {
		t.Fatal("expected error for limit 0")
	}
	if _, err := NewWorkerPoolLimiter(-1); err == nil {
		t.Fatal("expected error for negative limit")
	}
	if _, err := NewWorkerPoolLimiter(1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// WaitForPermit must actually block once the limit is exhausted, and unblock
// the instant a permit is released -- this is the property the buffered
// dispatcher relies on to cap in-flight concurrency at N.
func TestWorkerPoolLimiter_WaitForPermitBlocksAtLimit(t *testing.T) {
	limiter, err := NewWorkerPoolLimiter(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ctx := context.Background()

	if err := limiter.WaitForPermit(ctx, "first"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		if err := limiter.WaitForPermit(ctx, "second"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatal("second WaitForPermit returned before the first permit was released")
	case <-time.After(50 * time.Millisecond):
	}

	if err := limiter.ReleasePermit(ctx, "first"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("second WaitForPermit did not unblock after release")
	}
}

func TestWorkerPoolLimiter_WaitForPermitRespectsCtxCancel(t *testing.T) {
	limiter, err := NewWorkerPoolLimiter(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ctx := context.Background()
	if err := limiter.WaitForPermit(ctx, "holder"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := limiter.WaitForPermit(cancelCtx, "blocked"); err == nil {
		t.Fatal("expected context cancellation error")
	}
}

// -race must never report a data race on p.owners under concurrent
// acquire/release from many goroutines against a limit well below the count.
func TestWorkerPoolLimiter_ConcurrentAcquireRelease(t *testing.T) {
	const limit = 8
	const workers = 64

	limiter, err := NewWorkerPoolLimiter(limit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			for range 10 {
				if err := limiter.WaitForPermit(ctx, owner); err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				if err := limiter.ReleasePermit(ctx, owner); err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
			}
		}(strconv.Itoa(i))
	}
	wg.Wait()
}
