package broker

import (
	"context"
	"errors"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func declareTopologyOnMock(t *testing.T) *mockChannel {
	t.Helper()
	ch := &mockChannel{}
	conn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return ch, nil }}
	c := newClientWithAMQP(testConfig(), conn, nil)
	if err := c.DeclareTopology(context.Background()); err != nil {
		t.Fatalf("DeclareTopology: %v", err)
	}
	return ch
}

func TestDeclareTopology_ExchangesAreTopic(t *testing.T) {
	ch := declareTopologyOnMock(t)

	want := map[string]bool{
		"contractpro.events":    false,
		"contractpro.responses": false,
		"contractpro.commands":  false,
		"contractpro.dlx":       false,
	}
	for _, ex := range ch.exchanges {
		if ex.kind != exchangeKindTopic {
			t.Errorf("exchange %q kind = %q, want %q (fanout DLX would storm)", ex.name, ex.kind, exchangeKindTopic)
		}
		if _, ok := want[ex.name]; ok {
			want[ex.name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("exchange %q was not declared", name)
		}
	}
}

func TestDeclareTopology_AllQueuesDeclared(t *testing.T) {
	ch := declareTopologyOnMock(t)

	// 6 main + 6*3 retry = 24 queues.
	if len(ch.queues) != 24 {
		t.Fatalf("declared %d queues, want 24", len(ch.queues))
	}

	got := make(map[string]declaredQueue, len(ch.queues))
	for _, q := range ch.queues {
		got[q.name] = q
		// §6.2 frozen contract: every LIC queue is durable, NOT
		// auto-delete, NOT exclusive (code-reviewer M1).
		if !q.durable {
			t.Errorf("queue %q must be durable", q.name)
		}
		if q.autoDelete {
			t.Errorf("queue %q must NOT be auto-delete (§6.2)", q.name)
		}
		if q.exclusive {
			t.Errorf("queue %q must NOT be exclusive (§6.2)", q.name)
		}
	}

	mains := []string{
		"lic.q.version-artifacts-ready",
		"lic.q.version-created",
		"lic.q.artifacts-provided",
		"lic.q.lic-persist-confirm",
		"lic.q.lic-persist-fail",
		"lic.q.user-confirmed-type",
	}
	for _, m := range mains {
		if _, ok := got[m]; !ok {
			t.Errorf("main queue %q not declared", m)
		}
		for lvl := 1; lvl <= 3; lvl++ {
			rq := retryQueueName(m, lvl)
			if _, ok := got[rq]; !ok {
				t.Errorf("retry queue %q not declared", rq)
			}
		}
	}
}

func TestDeclareTopology_MainQueueArgs(t *testing.T) {
	ch := declareTopologyOnMock(t)

	for _, q := range ch.queues {
		if q.name != "lic.q.version-artifacts-ready" {
			continue
		}
		if got := q.args["x-message-ttl"]; got != mainQueueMessageTTLMillis {
			t.Errorf("x-message-ttl = %v, want %v (24h)", got, mainQueueMessageTTLMillis)
		}
		if got := q.args["x-max-length"]; got != mainQueueMaxLength {
			t.Errorf("x-max-length = %v, want %v", got, mainQueueMaxLength)
		}
		if got := q.args["x-dead-letter-exchange"]; got != "contractpro.dlx" {
			t.Errorf("x-dead-letter-exchange = %v, want contractpro.dlx", got)
		}
		// Critical: NO static x-dead-letter-routing-key on the main queue
		// (escalation is the consumer's dynamic decision, code-architect Q3).
		if _, present := q.args["x-dead-letter-routing-key"]; present {
			t.Error("main queue must NOT set a static x-dead-letter-routing-key")
		}
		return
	}
	t.Fatal("main queue lic.q.version-artifacts-ready not found")
}

func TestDeclareTopology_RetryQueueTTLs(t *testing.T) {
	ch := declareTopologyOnMock(t)

	wantTTL := map[string]int32{
		"lic.q.version-artifacts-ready.retry.1": 2_000,
		"lic.q.version-artifacts-ready.retry.2": 10_000,
		"lic.q.version-artifacts-ready.retry.3": 60_000,
	}
	seen := 0
	for _, q := range ch.queues {
		want, ok := wantTTL[q.name]
		if !ok {
			continue
		}
		seen++
		if got := q.args["x-message-ttl"]; got != want {
			t.Errorf("%s x-message-ttl = %v, want %d", q.name, got, want)
		}
		if got := q.args["x-dead-letter-exchange"]; got != "contractpro.dlx" {
			t.Errorf("%s x-dead-letter-exchange = %v", q.name, got)
		}
		// Retry queue returns to main via the ORIGINAL routing key.
		if got := q.args["x-dead-letter-routing-key"]; got != "dm.events.version-artifacts-ready" {
			t.Errorf("%s x-dead-letter-routing-key = %v, want dm.events.version-artifacts-ready", q.name, got)
		}
	}
	if seen != 3 {
		t.Fatalf("found %d of 3 expected retry queues", seen)
	}
}

func TestDeclareTopology_Bindings(t *testing.T) {
	ch := declareTopologyOnMock(t)

	has := func(q, key, ex string) bool {
		for _, b := range ch.binds {
			if b.queue == q && b.key == key && b.exchange == ex {
				return true
			}
		}
		return false
	}

	// Inbound: source exchange → main queue on the topic key.
	if !has("lic.q.version-artifacts-ready", "dm.events.version-artifacts-ready", "contractpro.events") {
		t.Error("missing inbound binding main<-events")
	}
	// Retry-return: DLX → main queue on the same topic key.
	if !has("lic.q.version-artifacts-ready", "dm.events.version-artifacts-ready", "contractpro.dlx") {
		t.Error("missing retry-return binding main<-dlx")
	}
	// Escalation: DLX → retry queue on its dedicated retry key.
	if !has("lic.q.version-artifacts-ready.retry.1", "lic.q.version-artifacts-ready.retry.1", "contractpro.dlx") {
		t.Error("missing escalation binding retry.1<-dlx")
	}
	// Commands subscription routes through the commands exchange.
	if !has("lic.q.user-confirmed-type", "orch.commands.user-confirmed-type", "contractpro.commands") {
		t.Error("missing inbound binding user-confirmed-type<-commands")
	}
}

func TestDeclareTopology_Idempotent_ArgsByteIdentical(t *testing.T) {
	ch := &mockChannel{}
	conn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return ch, nil }}
	c := newClientWithAMQP(testConfig(), conn, nil)

	if err := c.DeclareTopology(context.Background()); err != nil {
		t.Fatalf("first declare: %v", err)
	}
	first := append([]declaredQueue(nil), ch.queues...)
	ch.queues = nil
	if err := c.DeclareTopology(context.Background()); err != nil {
		t.Fatalf("second declare: %v", err)
	}

	if len(first) != len(ch.queues) {
		t.Fatalf("declare not idempotent: %d vs %d queues", len(first), len(ch.queues))
	}
	for i := range first {
		if first[i].name != ch.queues[i].name {
			t.Fatalf("queue order changed: %q vs %q", first[i].name, ch.queues[i].name)
		}
		for k, v := range first[i].args {
			if ch.queues[i].args[k] != v {
				t.Errorf("queue %q arg %q drifted: %v vs %v (would cause 406 on reconnect)", first[i].name, k, v, ch.queues[i].args[k])
			}
		}
	}
}

func TestTTLMillisInt32_ClampsOutOfRange(t *testing.T) {
	// In-range values pass through.
	if got := ttlMillisInt32(2 * time.Second); got != 2000 {
		t.Errorf("2s → %d, want 2000", got)
	}
	// Non-positive and overflowing values clamp to the 24h main TTL rather
	// than overflowing int32 to a negative x-message-ttl (security MF-2).
	if got := ttlMillisInt32(0); got != mainQueueMessageTTLMillis {
		t.Errorf("0 → %d, want clamp %d", got, mainQueueMessageTTLMillis)
	}
	if got := ttlMillisInt32(-time.Second); got != mainQueueMessageTTLMillis {
		t.Errorf("negative → %d, want clamp", got)
	}
	if got := ttlMillisInt32(40 * 24 * time.Hour); got != mainQueueMessageTTLMillis {
		t.Errorf("40d (overflows int32 ms) → %d, want clamp %d", got, mainQueueMessageTTLMillis)
	}
}

func TestDeclareTopology_NotConnected(t *testing.T) {
	c := newClientWithAMQP(testConfig(), nil, nil)
	err := c.DeclareTopology(context.Background())
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("want ErrNotConnected, got %v", err)
	}
}

func TestDeclareTopology_ContextCancelled(t *testing.T) {
	conn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return &mockChannel{}, nil }}
	c := newClientWithAMQP(testConfig(), conn, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.DeclareTopology(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestDeclareTopology_DeclareError_NonRetryable(t *testing.T) {
	ch := &mockChannel{
		queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
			return amqp.Queue{}, &amqp.Error{Code: 406, Reason: "PRECONDITION_FAILED"}
		},
	}
	conn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return ch, nil }}
	c := newClientWithAMQP(testConfig(), conn, nil)

	err := c.DeclareTopology(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if IsRetryable(err) {
		t.Error("406 PRECONDITION_FAILED must be non-retryable")
	}
}
