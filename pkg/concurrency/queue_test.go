package concurrency

import (
	"context"
	"testing"
	"time"
)

func TestNewPressureQueue_ValidatesLimit(t *testing.T) {
	if _, err := NewPressureQueue[int](0); err == nil {
		t.Fatal("expected error for limit 0")
	}
	if _, err := NewPressureQueue[int](-1); err == nil {
		t.Fatal("expected error for negative limit")
	}
}

func TestPressureQueue_WaitForRoomReturnsImmediatelyWhenThresholdMet(t *testing.T) {
	q, err := NewPressureQueue[int](10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	start := time.Now()
	room, err := q.WaitForRoom(context.Background(), time.Second, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if room != 10 {
		t.Fatalf("room = %d, want 10 (empty queue)", room)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("WaitForRoom took %v, expected an immediate return", elapsed)
	}
}

// below threshold, WaitForRoom blocks until the debounce timer fires -- the
// prefetch loop's only exit when nothing is ever dequeued to signal room.
func TestPressureQueue_WaitForRoomDebouncesWhenBelowThreshold(t *testing.T) {
	q, err := NewPressureQueue[int](2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	one := 1
	if err := q.EnQueue(context.Background(), &one); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	start := time.Now()
	room, err := q.WaitForRoom(context.Background(), 100*time.Millisecond, 2) // cap(2)-len(1)=1 < threshold(2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if room != 1 {
		t.Fatalf("room = %d, want 1", room)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("WaitForRoom returned after %v, expected to wait out the debounce", elapsed)
	}
}

// a DeQueue while WaitForRoom is blocked wakes it immediately instead of
// waiting out the full debounce -- this is what dequeuedSignal is for.
func TestPressureQueue_WaitForRoomWakesOnDequeue(t *testing.T) {
	q, err := NewPressureQueue[int](1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	one := 1
	if err := q.EnQueue(context.Background(), &one); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := q.WaitForRoom(context.Background(), 5*time.Second, 1); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}()

	time.Sleep(20 * time.Millisecond) // let WaitForRoom start blocking
	if _, err := q.DeQueue(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WaitForRoom did not wake on DeQueue")
	}
}

func TestPressureQueue_EnQueueBlocksWhenFull(t *testing.T) {
	q, err := NewPressureQueue[int](1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	one := 1
	if err := q.EnQueue(context.Background(), &one); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	blocked := make(chan struct{})
	two := 2
	go func() {
		if err := q.EnQueue(context.Background(), &two); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		close(blocked)
	}()

	select {
	case <-blocked:
		t.Fatal("EnQueue returned before room was made")
	case <-time.After(50 * time.Millisecond):
	}

	if _, err := q.DeQueue(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-blocked:
	case <-time.After(time.Second):
		t.Fatal("EnQueue did not unblock after DeQueue made room")
	}
}

func TestPressureQueue_DeQueueRespectsCtxCancel(t *testing.T) {
	q, err := NewPressureQueue[int](1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := q.DeQueue(ctx); err == nil {
		t.Fatal("expected context cancellation error")
	}
}
