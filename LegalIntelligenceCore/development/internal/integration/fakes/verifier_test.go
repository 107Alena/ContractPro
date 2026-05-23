package fakes

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// recordingTb captures Fatalf invocations on a Tb without aborting; tests
// that exercise verifier failure paths use it to keep their own *testing.T
// green even when AssertPublished et al. intentionally fail.
type recordingTb struct {
	mu      sync.Mutex
	fatalf  []string
	helpers int
	failed  bool
}

func (r *recordingTb) Helper() {
	r.mu.Lock()
	r.helpers++
	r.mu.Unlock()
}

func (r *recordingTb) Fatalf(format string, args ...any) {
	r.mu.Lock()
	r.fatalf = append(r.fatalf, fmt.Sprintf(format, args...))
	r.failed = true
	r.mu.Unlock()
	// Real *testing.T.Fatalf aborts via Goexit. recordingTb intentionally
	// does NOT — the verifier helpers under test return after the Fatalf
	// call, and the recordingTb captures the message for assertion.
	runtime.Goexit()
}

// runWithRecordingTb invokes fn in a goroutine driven by rec; returns the
// captured rec after the goroutine exits (Goexit from rec.Fatalf is the
// usual termination).
func runWithRecordingTb(fn func(rec *recordingTb)) *recordingTb {
	rec := &recordingTb{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(rec)
	}()
	<-done
	return rec
}

func TestAssertPublished_SuccessReturnsMessage(t *testing.T) {
	fb := NewFakeBroker()
	_ = fb.Publish(context.Background(), "ex", "rk", []byte("p"))
	got := AssertPublished(t, fb, "rk")
	if string(got.Payload) != "p" {
		t.Fatalf("got %q", got.Payload)
	}
}

func TestAssertPublished_FailureRecordsFatalf(t *testing.T) {
	fb := NewFakeBroker()
	_ = fb.Publish(context.Background(), "ex", "other", []byte("x"))
	rec := runWithRecordingTb(func(rec *recordingTb) {
		AssertPublished(rec, fb, "expected")
	})
	if !rec.failed {
		t.Fatal("expected Fatalf to fire")
	}
}

func TestAssertNotPublished_Success(t *testing.T) {
	fb := NewFakeBroker()
	_ = fb.Publish(context.Background(), "ex", "other", []byte("x"))
	AssertNotPublished(t, fb, "absent")
}

func TestAssertNotPublished_Failure(t *testing.T) {
	fb := NewFakeBroker()
	_ = fb.Publish(context.Background(), "ex", "present", []byte("x"))
	rec := runWithRecordingTb(func(rec *recordingTb) {
		AssertNotPublished(rec, fb, "present")
	})
	if !rec.failed {
		t.Fatal("expected Fatalf to fire")
	}
}

func TestWaitForPublish_FindsRecorded(t *testing.T) {
	fb := NewFakeBroker()
	_ = fb.Publish(context.Background(), "ex", "rk", []byte("p"))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	msg, err := WaitForPublish(ctx, fb, "rk")
	if err != nil || string(msg.Payload) != "p" {
		t.Fatalf("got %+v %v", msg, err)
	}
}

func TestWaitForPublish_TimeoutWraps(t *testing.T) {
	fb := NewFakeBroker()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := WaitForPublish(ctx, fb, "absent")
	if !IsTimeout(err) {
		t.Fatalf("expected timeout, got %v", err)
	}
}

func TestWaitForPublishAfter_SinceCursor(t *testing.T) {
	fb := NewFakeBroker()
	_ = fb.Publish(context.Background(), "ex", "rk", []byte("old"))
	cursor := time.Now()
	time.Sleep(2 * time.Millisecond)
	_ = fb.Publish(context.Background(), "ex", "rk", []byte("new"))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	msg, err := WaitForPublishAfter(ctx, fb, "rk", cursor)
	if err != nil {
		t.Fatal(err)
	}
	if string(msg.Payload) != "new" {
		t.Fatalf("got %q", msg.Payload)
	}
}

func TestMatchEvent_Decodes(t *testing.T) {
	fb := NewFakeBroker()
	_ = fb.Publish(context.Background(), "ex", "rk", []byte(`{"job_id":"j"}`))
	msg := fb.PublishedOn("rk")[0]
	var evt struct {
		JobID string `json:"job_id"`
	}
	MatchEvent(t, msg, &evt)
	if evt.JobID != "j" {
		t.Fatalf("decode: %+v", evt)
	}
}

func TestAssertPayloadField_OK(t *testing.T) {
	fb := NewFakeBroker()
	_ = fb.Publish(context.Background(), "ex", "rk", []byte(`{"status":"OK"}`))
	msg := fb.PublishedOn("rk")[0]
	AssertPayloadField(t, msg, "status", "OK")
}

func TestAssertPayloadField_MismatchFails(t *testing.T) {
	fb := NewFakeBroker()
	_ = fb.Publish(context.Background(), "ex", "rk", []byte(`{"status":"OK"}`))
	msg := fb.PublishedOn("rk")[0]
	rec := runWithRecordingTb(func(rec *recordingTb) {
		AssertPayloadField(rec, msg, "status", "NO")
	})
	if !rec.failed {
		t.Fatal("expected Fatalf")
	}
}

func TestAssertPayloadField_KeyAbsentFails(t *testing.T) {
	fb := NewFakeBroker()
	_ = fb.Publish(context.Background(), "ex", "rk", []byte(`{"other":1}`))
	msg := fb.PublishedOn("rk")[0]
	rec := runWithRecordingTb(func(rec *recordingTb) {
		AssertPayloadField(rec, msg, "status", "OK")
	})
	if !rec.failed {
		t.Fatal("expected Fatalf")
	}
}
