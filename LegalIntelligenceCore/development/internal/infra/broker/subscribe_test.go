package broker

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestSubscribe_AppliesQosAndConsumes(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery, 1)
	var consumedQueue string
	var autoAckSeen bool
	ch := &mockChannel{
		consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
			consumedQueue = queue
			autoAckSeen = autoAck
			return deliveryCh, nil
		},
	}
	conn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return ch, nil }}
	c := newClientWithAMQP(testConfig(), conn, nil)

	got := make(chan Delivery, 1)
	if err := c.Subscribe("lic.q.version-artifacts-ready", func(ctx context.Context, d Delivery) error {
		got <- d
		return d.Ack()
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if ch.qosCount != 10 {
		t.Errorf("Qos prefetch = %d, want 10", ch.qosCount)
	}
	if consumedQueue != "lic.q.version-artifacts-ready" {
		t.Errorf("consumed %q", consumedQueue)
	}
	if autoAckSeen {
		t.Error("auto-ack must be false (manual ack)")
	}

	deliveryCh <- amqp.Delivery{Acknowledger: &mockAcknowledger{}, Body: []byte(`{"job":"1"}`)}
	select {
	case d := <-got:
		if string(d.Body()) != `{"job":"1"}` {
			t.Errorf("body = %q", d.Body())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not receive delivery")
	}

	if len(c.subs) != 1 {
		t.Errorf("subscription not recorded for reconnect (subs=%d)", len(c.subs))
	}
	close(deliveryCh)
	_ = c.Close()
}

func TestSubscribe_HandlerOwnsAckNackReject(t *testing.T) {
	for _, tc := range []struct {
		name string
		act  func(d Delivery) error
		want string // "ack" | "nack" | "reject"
	}{
		{"ack", func(d Delivery) error { return d.Ack() }, "ack"},
		{"nack", func(d Delivery) error { return d.Nack(false) }, "nack"},
		{"reject", func(d Delivery) error { return d.Reject(false) }, "reject"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deliveryCh := make(chan amqp.Delivery, 1)
			ch := &mockChannel{consumeFn: func(q, cs string, aa, ex, nl, nw bool, a amqp.Table) (<-chan amqp.Delivery, error) {
				return deliveryCh, nil
			}}
			conn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return ch, nil }}
			c := newClientWithAMQP(testConfig(), conn, nil)

			result := make(chan string, 1)
			acker := &mockAcknowledger{
				ackFn:    func(tag uint64, multiple bool) error { result <- "ack"; return nil },
				nackFn:   func(tag uint64, multiple, requeue bool) error { result <- "nack"; return nil },
				rejectFn: func(tag uint64, requeue bool) error { result <- "reject"; return nil },
			}

			if err := c.Subscribe("q", func(ctx context.Context, d Delivery) error { return tc.act(d) }); err != nil {
				t.Fatalf("Subscribe: %v", err)
			}
			deliveryCh <- amqp.Delivery{Acknowledger: acker, Body: []byte("x")}

			select {
			case got := <-result:
				if got != tc.want {
					t.Errorf("got %q, want %q", got, tc.want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("acknowledger not invoked")
			}
			close(deliveryCh)
			_ = c.Close()
		})
	}
}

func TestDelivery_HeadersAndXDeath(t *testing.T) {
	xdeath := []any{
		amqp.Table{
			"queue":        "lic.q.version-artifacts-ready",
			"reason":       "rejected",
			"exchange":     "contractpro.dlx",
			"count":        int64(2),
			"routing-keys": []any{"lic.q.version-artifacts-ready.retry.2"},
		},
		"not-a-table-skip-me",
	}
	d := &amqpDelivery{d: amqp.Delivery{
		Body: []byte("body"),
		Headers: amqp.Table{
			"x-correlation-id": "abc",
			"x-death":          xdeath,
		},
	}}

	if v, ok := d.Header("x-correlation-id"); !ok || v != "abc" {
		t.Errorf("Header() = %v,%v", v, ok)
	}
	if _, ok := d.Header("missing"); ok {
		t.Error("missing header reported present")
	}
	if !reflect.DeepEqual(d.Headers()["x-correlation-id"], "abc") {
		t.Error("Headers() lost x-correlation-id")
	}

	entries := d.XDeath()
	if len(entries) != 1 {
		t.Fatalf("XDeath len = %d, want 1 (malformed entry skipped)", len(entries))
	}
	e := entries[0]
	if e.Queue != "lic.q.version-artifacts-ready" || e.Reason != "rejected" ||
		e.Exchange != "contractpro.dlx" || e.Count != 2 ||
		len(e.RoutingKeys) != 1 || e.RoutingKeys[0] != "lic.q.version-artifacts-ready.retry.2" {
		t.Errorf("decoded x-death wrong: %+v", e)
	}
}

func TestDelivery_XDeath_AbsentOrMalformed(t *testing.T) {
	if (&amqpDelivery{d: amqp.Delivery{}}).XDeath() != nil {
		t.Error("nil headers → XDeath should be nil")
	}
	d := &amqpDelivery{d: amqp.Delivery{Headers: amqp.Table{"x-death": "garbage"}}}
	if d.XDeath() != nil {
		t.Error("malformed x-death → nil")
	}
}

func TestDelivery_XDeath_CountEncodings(t *testing.T) {
	for _, v := range []any{int64(3), int32(3), int(3), uint(3), uint32(3), uint64(3), float64(3)} {
		d := &amqpDelivery{d: amqp.Delivery{Headers: amqp.Table{
			"x-death": []any{amqp.Table{"queue": "q", "count": v}},
		}}}
		got := d.XDeath()
		if len(got) != 1 || got[0].Count != 3 {
			t.Errorf("count encoding %T → %+v", v, got)
		}
	}
	// Unknown encoding must decode as "exhausted" (huge), NOT 0 — a wrong 0
	// would pin the message to retry.1 forever (code-reviewer S5).
	d := &amqpDelivery{d: amqp.Delivery{Headers: amqp.Table{
		"x-death": []any{amqp.Table{"queue": "q", "count": "weird"}},
	}}}
	if got := d.XDeath(); len(got) != 1 || got[0].Count < 1<<40 {
		t.Errorf("unknown count encoding must be treated as exhausted, got %+v", got)
	}
}

func TestDelivery_XDeath_CapsEntriesAndRoutingKeys(t *testing.T) {
	entries := make([]any, xDeathMaxEntries+50)
	rks := make([]any, xDeathMaxRoutingKeys+50)
	for i := range rks {
		rks[i] = "rk"
	}
	for i := range entries {
		entries[i] = amqp.Table{"queue": "q", "count": int64(1), "routing-keys": rks}
	}
	d := &amqpDelivery{d: amqp.Delivery{Headers: amqp.Table{"x-death": entries}}}
	got := d.XDeath()
	if len(got) != xDeathMaxEntries {
		t.Errorf("entries not capped: %d", len(got))
	}
	if len(got[0].RoutingKeys) != xDeathMaxRoutingKeys {
		t.Errorf("routing-keys not capped: %d", len(got[0].RoutingKeys))
	}
}

func TestDelivery_HeadersReturnsCopy(t *testing.T) {
	orig := amqp.Table{"k": "v"}
	d := &amqpDelivery{d: amqp.Delivery{Headers: orig}}
	h := d.Headers()
	h["k"] = "TAMPERED"
	h["injected"] = true
	if orig["k"] != "v" {
		t.Error("Headers() must return a copy; live delivery state was mutated")
	}
	if _, ok := orig["injected"]; ok {
		t.Error("Headers() copy must not write back into the delivery")
	}
}

func TestSubscribe_NotConnected(t *testing.T) {
	c := newClientWithAMQP(testConfig(), nil, nil)
	if err := c.Subscribe("q", func(ctx context.Context, d Delivery) error { return nil }); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("want ErrNotConnected, got %v", err)
	}
}

func TestSubscribe_ConsumeError(t *testing.T) {
	ch := &mockChannel{consumeFn: func(q, cs string, aa, ex, nl, nw bool, a amqp.Table) (<-chan amqp.Delivery, error) {
		return nil, &amqp.Error{Code: 404, Reason: "NOT_FOUND"}
	}}
	conn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return ch, nil }}
	c := newClientWithAMQP(testConfig(), conn, nil)

	err := c.Subscribe("q", func(ctx context.Context, d Delivery) error { return nil })
	if err == nil {
		t.Fatal("expected error")
	}
	if IsRetryable(err) {
		t.Error("404 NOT_FOUND must be non-retryable")
	}
	if len(c.subs) != 0 {
		t.Error("failed Subscribe must not record a subscription")
	}
}
