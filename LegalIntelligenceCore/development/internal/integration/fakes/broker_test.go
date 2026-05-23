package fakes

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/infra/broker"
)

func TestNewFakeBrokerWithLICTopology_PresetBindings(t *testing.T) {
	fb := NewFakeBrokerWithLICTopology()
	for _, b := range LICTopologyBindings() {
		queue, routingKey := b[0], b[1]
		if _, ok := fb.bindings[routingKey][queue]; !ok {
			t.Fatalf("expected binding %s→%s", routingKey, queue)
		}
	}
}

func TestSubscribe_RejectsNilHandler(t *testing.T) {
	fb := NewFakeBroker()
	if err := fb.Subscribe("q", nil); err == nil {
		t.Fatal("expected nil-handler error")
	}
}

func TestSubscribe_RejectsClosedBroker(t *testing.T) {
	fb := NewFakeBroker()
	_ = fb.Close()
	if err := fb.Subscribe("q", func(context.Context, broker.Delivery) error { return nil }); err == nil {
		t.Fatal("expected closed-broker error")
	}
}

func TestPublish_RecordsAndReturnsCopy(t *testing.T) {
	fb := NewFakeBroker()
	payload := []byte(`{"k":1}`)
	if err := fb.Publish(context.Background(), "ex", "rk", payload); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// Mutate the original; the recorded copy must be intact.
	payload[0] = '#'
	msgs := fb.Published()
	if len(msgs) != 1 {
		t.Fatalf("len(Published)=%d", len(msgs))
	}
	if string(msgs[0].Payload) != `{"k":1}` {
		t.Fatalf("payload mutated: %s", msgs[0].Payload)
	}
	if msgs[0].Exchange != "ex" || msgs[0].RoutingKey != "rk" {
		t.Fatalf("exchange/rk mismatch")
	}
}

func TestPublish_HonorsCtxError(t *testing.T) {
	fb := NewFakeBroker()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := fb.Publish(ctx, "ex", "rk", []byte("x"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected Canceled, got %v", err)
	}
}

func TestPublish_EmptyPayloadFails(t *testing.T) {
	fb := NewFakeBroker()
	err := fb.Publish(context.Background(), "ex", "rk", nil)
	if err == nil {
		t.Fatal("expected error on empty payload")
	}
}

func TestInjectPublishError_FIFO(t *testing.T) {
	fb := NewFakeBroker()
	a := errors.New("a")
	b := errors.New("b")
	fb.InjectPublishError(a)
	fb.InjectPublishError(b)
	if err := fb.Publish(context.Background(), "ex", "rk", []byte("1")); err != a {
		t.Fatalf("first err: got %v want %v", err, a)
	}
	if err := fb.Publish(context.Background(), "ex", "rk", []byte("2")); err != b {
		t.Fatalf("second err: got %v want %v", err, b)
	}
	// After draining, normal publish succeeds.
	if err := fb.Publish(context.Background(), "ex", "rk", []byte("3")); err != nil {
		t.Fatalf("third: %v", err)
	}
	if got := fb.Published(); len(got) != 1 {
		t.Fatalf("expected only the third publish recorded, got %d", len(got))
	}
}

func TestInject_FansOutToAllSubscribers(t *testing.T) {
	fb := NewFakeBrokerWithLICTopology()
	var hits atomic.Int64
	handler := func(ctx context.Context, d broker.Delivery) error {
		hits.Add(1)
		return d.Ack()
	}
	if err := fb.Subscribe(QueueVersionArtifactsReady, handler); err != nil {
		t.Fatal(err)
	}
	if err := fb.Subscribe(QueueVersionArtifactsReady, handler); err != nil {
		t.Fatal(err)
	}
	res, err := fb.Inject(context.Background(), RoutingKeyVersionArtifactsReady, nil, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Handlers != 2 || res.Acked != 2 {
		t.Fatalf("inject: %+v", res)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits=%d", hits.Load())
	}
}

func TestInject_UnknownRoutingKey_NoHandlers(t *testing.T) {
	fb := NewFakeBrokerWithLICTopology()
	res, err := fb.Inject(context.Background(), "nonexistent.key", nil, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Handlers != 0 {
		t.Fatalf("expected 0 handlers, got %d", res.Handlers)
	}
}

func TestInject_DeliveryAckNackReject(t *testing.T) {
	cases := []struct {
		name string
		fn   func(d broker.Delivery) error
		check func(*testing.T, InjectResult)
	}{
		{"ack", func(d broker.Delivery) error { return d.Ack() },
			func(t *testing.T, r InjectResult) { t.Helper(); if r.Acked != 1 { t.Fatalf("acked=%d", r.Acked) } }},
		{"nack-false", func(d broker.Delivery) error { return d.Nack(false) },
			func(t *testing.T, r InjectResult) { t.Helper(); if r.Nacked != 1 { t.Fatalf("nacked=%d", r.Nacked) } }},
		{"reject-true", func(d broker.Delivery) error { return d.Reject(true) },
			func(t *testing.T, r InjectResult) { t.Helper(); if r.Rejected != 1 { t.Fatalf("rejected=%d", r.Rejected) } }},
		{"unterminated", func(d broker.Delivery) error { return nil },
			func(t *testing.T, r InjectResult) { t.Helper(); if r.Unterminated != 1 { t.Fatalf("unterm=%d", r.Unterminated) } }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fb := NewFakeBrokerWithLICTopology()
			if err := fb.Subscribe(QueueVersionCreated, func(ctx context.Context, d broker.Delivery) error {
				return tc.fn(d)
			}); err != nil {
				t.Fatal(err)
			}
			res, _ := fb.Inject(context.Background(), RoutingKeyVersionCreated, nil, []byte("x"))
			tc.check(t, res)
		})
	}
}

func TestFakeDelivery_DoubleTerminateErrors(t *testing.T) {
	d := newFakeDelivery([]byte("x"), nil)
	if err := d.Ack(); err != nil {
		t.Fatal(err)
	}
	if err := d.Ack(); !errors.Is(err, ErrAlreadyTerminated) {
		t.Fatalf("expected already-terminated, got %v", err)
	}
	if err := d.Nack(false); !errors.Is(err, ErrAlreadyTerminated) {
		t.Fatalf("nack after ack: %v", err)
	}
}

func TestFakeDelivery_HeadersCopyIsolated(t *testing.T) {
	hdr := map[string]any{"k": "v"}
	d := newFakeDelivery([]byte("x"), hdr)
	cp := d.Headers()
	cp["k"] = "MUTATED"
	if v, _ := d.Header("k"); v != "v" {
		t.Fatalf("header leaked through copy: %v", v)
	}
}

func TestFakeDelivery_XDeath_TypedSlice(t *testing.T) {
	hdr := XDeathHeader("lic.q.version-artifacts-ready", "dm.events.version-artifacts-ready", 2)
	d := newFakeDelivery([]byte("x"), hdr)
	got := d.XDeath()
	if len(got) != 1 || got[0].Count != 2 {
		t.Fatalf("XDeath: %+v", got)
	}
	if got[0].Queue != "lic.q.version-artifacts-ready" {
		t.Fatalf("queue: %s", got[0].Queue)
	}
	if len(got[0].RoutingKeys) != 1 || got[0].RoutingKeys[0] != "dm.events.version-artifacts-ready" {
		t.Fatalf("rks: %v", got[0].RoutingKeys)
	}
}

func TestFakeDelivery_XDeath_WireFaithfulShape(t *testing.T) {
	hdr := map[string]any{
		"x-death": []any{
			map[string]any{
				"queue":  "q",
				"reason": "rejected",
				"count":  int64(1),
				"routing-keys": []any{"rk"},
			},
		},
	}
	d := newFakeDelivery([]byte("x"), hdr)
	got := d.XDeath()
	if len(got) != 1 || got[0].Count != 1 || got[0].Queue != "q" {
		t.Fatalf("XDeath wire-shape: %+v", got)
	}
}

func TestFakeDelivery_XDeath_AbsentReturnsNil(t *testing.T) {
	d := newFakeDelivery([]byte("x"), nil)
	if got := d.XDeath(); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestOnPublish_ListenerFiresAfterRecord(t *testing.T) {
	fb := NewFakeBroker()
	var got PublishedMessage
	var wg sync.WaitGroup
	wg.Add(1)
	fb.OnPublish("rk", func(msg PublishedMessage) {
		got = msg
		wg.Done()
	})
	if err := fb.Publish(context.Background(), "ex", "rk", []byte("y")); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if string(got.Payload) != "y" || got.RoutingKey != "rk" {
		t.Fatalf("listener: %+v", got)
	}
}

func TestOnPublish_WildcardMatchesAll(t *testing.T) {
	fb := NewFakeBroker()
	var n atomic.Int64
	fb.OnPublish("", func(PublishedMessage) { n.Add(1) })
	_ = fb.Publish(context.Background(), "ex", "rk1", []byte("a"))
	_ = fb.Publish(context.Background(), "ex", "rk2", []byte("b"))
	if n.Load() != 2 {
		t.Fatalf("wildcard hits=%d", n.Load())
	}
}

func TestResetPublished(t *testing.T) {
	fb := NewFakeBroker()
	_ = fb.Publish(context.Background(), "ex", "rk", []byte("x"))
	fb.ResetPublished()
	if len(fb.Published()) != 0 {
		t.Fatal("expected empty after reset")
	}
}

func TestClose_Idempotent(t *testing.T) {
	fb := NewFakeBroker()
	if err := fb.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fb.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if !fb.Closed() {
		t.Fatal("Closed() should be true")
	}
}

func TestSubscribe_ConcurrentFanOut(t *testing.T) {
	fb := NewFakeBrokerWithLICTopology()
	var hits atomic.Int64
	const N = 8
	for i := 0; i < N; i++ {
		if err := fb.Subscribe(QueueArtifactsProvided, func(ctx context.Context, d broker.Delivery) error {
			hits.Add(1)
			return d.Ack()
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fb.Inject(context.Background(), RoutingKeyArtifactsProvided, nil, []byte("p")); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != int64(N) {
		t.Fatalf("hits=%d", hits.Load())
	}
}

func TestPublishedOn_FiltersByRoutingKey(t *testing.T) {
	fb := NewFakeBroker()
	_ = fb.Publish(context.Background(), "ex", "rk1", []byte("a"))
	_ = fb.Publish(context.Background(), "ex", "rk2", []byte("b"))
	if got := fb.PublishedOn("rk1"); len(got) != 1 || string(got[0].Payload) != "a" {
		t.Fatalf("filter: %+v", got)
	}
}

// concurrency probe — race-clean under -race.
func TestFakeBroker_ConcurrentPublishAndOnPublish(t *testing.T) {
	fb := NewFakeBroker()
	var received atomic.Int64
	fb.OnPublish("", func(PublishedMessage) { received.Add(1) })

	const G = 16
	const N = 32
	var wg sync.WaitGroup
	for g := 0; g < G; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < N; i++ {
				_ = fb.Publish(context.Background(), "ex", "rk", []byte("x"))
			}
		}()
	}
	wg.Wait()
	if got := received.Load(); got != int64(G*N) {
		t.Fatalf("received=%d want=%d", got, G*N)
	}
	// Wait briefly for any straggler listener invocations spawned in the
	// publishing goroutine to settle — they run synchronously inside
	// Publish so this is just defensive belt-and-suspenders.
	time.Sleep(5 * time.Millisecond)
}
