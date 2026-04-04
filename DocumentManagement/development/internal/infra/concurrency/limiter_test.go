package concurrency

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- test logger ---

type testLogger struct {
	mu       sync.Mutex
	messages []string
}

func (l *testLogger) Debug(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, msg)
}

func (l *testLogger) Warn(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, msg)
}

func (l *testLogger) hasMessage(needle string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, m := range l.messages {
		if m == needle {
			return true
		}
	}
	return false
}

// --- constructor tests ---

func TestNewSemaphore_PanicsOnNilLogger(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil logger")
		}
	}()
	NewSemaphore(5, nil)
}

func TestNewSemaphore_DefaultsToOneIfZero(t *testing.T) {
	s := NewSemaphore(0, &testLogger{})
	if s.Capacity() != 1 {
		t.Errorf("expected capacity 1 for maxConcurrent=0, got %d", s.Capacity())
	}
}

func TestNewSemaphore_DefaultsToOneIfNegative(t *testing.T) {
	s := NewSemaphore(-5, &testLogger{})
	if s.Capacity() != 1 {
		t.Errorf("expected capacity 1 for maxConcurrent=-5, got %d", s.Capacity())
	}
}

func TestNewSemaphore_CorrectCapacity(t *testing.T) {
	s := NewSemaphore(10, &testLogger{})
	if s.Capacity() != 10 {
		t.Errorf("expected capacity 10, got %d", s.Capacity())
	}
}

// --- acquire/release tests ---

func TestAcquire_ImmediateWhenSlotsAvailable(t *testing.T) {
	s := NewSemaphore(3, &testLogger{})

	if err := s.Acquire(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.ActiveCount() != 1 {
		t.Errorf("expected active=1, got %d", s.ActiveCount())
	}
	s.Release()
	if s.ActiveCount() != 0 {
		t.Errorf("expected active=0 after release, got %d", s.ActiveCount())
	}
}

func TestAcquire_BlocksWhenFull(t *testing.T) {
	s := NewSemaphore(1, &testLogger{})

	if err := s.Acquire(context.Background()); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Second acquire should block.
	acquired := make(chan struct{})
	go func() {
		_ = s.Acquire(context.Background())
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatal("second acquire should block when semaphore is full")
	case <-time.After(50 * time.Millisecond):
		// expected: blocked
	}

	// Release first slot.
	s.Release()

	select {
	case <-acquired:
		// expected: unblocked
	case <-time.After(time.Second):
		t.Fatal("second acquire should succeed after release")
	}

	if s.ActiveCount() != 1 {
		t.Errorf("expected active=1, got %d", s.ActiveCount())
	}
	s.Release()
}

func TestAcquire_CancelledContext(t *testing.T) {
	s := NewSemaphore(1, &testLogger{})
	_ = s.Acquire(context.Background()) // fill the slot

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.Acquire(ctx)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if s.ActiveCount() != 1 {
		t.Errorf("expected active=1 (only first acquire), got %d", s.ActiveCount())
	}
	s.Release()
}

func TestAcquire_TimedOutContext(t *testing.T) {
	s := NewSemaphore(1, &testLogger{})
	_ = s.Acquire(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := s.Acquire(ctx)
	if err == nil {
		t.Fatal("expected error on timed out context")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
	s.Release()
}

func TestRelease_WithoutAcquire_LogsWarning(t *testing.T) {
	logger := &testLogger{}
	s := NewSemaphore(3, logger)

	s.Release()

	if !logger.hasMessage("semaphore release called without matching acquire") {
		t.Error("expected warning for release without acquire")
	}
	if s.ActiveCount() != 0 {
		t.Errorf("expected active=0, got %d", s.ActiveCount())
	}
}

func TestAcquire_FillAndDrain(t *testing.T) {
	s := NewSemaphore(3, &testLogger{})

	for i := 0; i < 3; i++ {
		if err := s.Acquire(context.Background()); err != nil {
			t.Fatalf("acquire %d failed: %v", i, err)
		}
	}
	if s.ActiveCount() != 3 {
		t.Errorf("expected active=3, got %d", s.ActiveCount())
	}

	for i := 0; i < 3; i++ {
		s.Release()
	}
	if s.ActiveCount() != 0 {
		t.Errorf("expected active=0, got %d", s.ActiveCount())
	}
}

// --- concurrency tests ---

func TestConcurrency_NeverExceedsCapacity(t *testing.T) {
	const maxConcurrent = 3
	const totalTasks = 50

	s := NewSemaphore(maxConcurrent, &testLogger{})
	var peak atomic.Int64
	var current atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < totalTasks; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Acquire(context.Background()); err != nil {
				return
			}
			defer s.Release()

			now := current.Add(1)
			for {
				old := peak.Load()
				if now <= old || peak.CompareAndSwap(old, now) {
					break
				}
			}
			time.Sleep(time.Millisecond) // simulate work
			current.Add(-1)
		}()
	}

	wg.Wait()

	if peak.Load() > maxConcurrent {
		t.Errorf("peak concurrent=%d exceeded capacity=%d", peak.Load(), maxConcurrent)
	}
	if s.ActiveCount() != 0 {
		t.Errorf("expected active=0 after all done, got %d", s.ActiveCount())
	}
}

func TestConcurrency_AllTasksComplete(t *testing.T) {
	const maxConcurrent = 2
	const totalTasks = 20

	s := NewSemaphore(maxConcurrent, &testLogger{})
	var completed atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < totalTasks; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Acquire(context.Background())
			defer s.Release()
			completed.Add(1)
		}()
	}

	wg.Wait()
	if completed.Load() != totalTasks {
		t.Errorf("expected %d completed, got %d", totalTasks, completed.Load())
	}
}

// --- logging tests ---

func TestAcquire_SlowPath_LogsDebug(t *testing.T) {
	logger := &testLogger{}
	s := NewSemaphore(1, logger)
	_ = s.Acquire(context.Background()) // fill

	done := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_ = s.Acquire(ctx) // will timeout
		close(done)
	}()

	<-done
	if !logger.hasMessage("semaphore full, waiting for slot") {
		t.Error("expected debug log when semaphore is full")
	}
	s.Release()
}

func TestAcquire_CancelledContext_LogsWarning(t *testing.T) {
	logger := &testLogger{}
	s := NewSemaphore(1, logger)
	_ = s.Acquire(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = s.Acquire(ctx)

	if !logger.hasMessage("semaphore acquire cancelled") {
		t.Error("expected warning log on cancelled acquire")
	}
	s.Release()
}
