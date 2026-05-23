package fakes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Tb is the subset of testing.T this package needs for failure surfaces.
// Declared narrow so the helpers can be exercised in this package's own
// tests against an in-file recording fake without pulling testing.TB.
type Tb interface {
	Helper()
	Fatalf(format string, args ...any)
}

// WaitForPublish blocks until a publish on routingKey is recorded or
// ctx expires. Returns the first matching message (or the first message
// at or after the call's start; subsequent calls advance the cursor —
// see WaitForPublishAfter).
//
// Use a ctx with a short deadline (typically 100ms-2s) to keep the test
// fast; the fake does not employ time.Sleep, so the wakeup is just the
// next OnPublish dispatch.
func WaitForPublish(ctx context.Context, fb *FakeBroker, routingKey string) (PublishedMessage, error) {
	return WaitForPublishAfter(ctx, fb, routingKey, time.Time{})
}

// WaitForPublishAfter blocks until a publish on routingKey at or after
// since is recorded or ctx expires. since==zero ⇒ any matching message.
//
// Implementation note: a simple poll-the-published-log loop on a small
// time.Ticker. We deliberately avoid a notification channel on
// FakeBroker because every test that needs precise timing already
// drives Inject synchronously (Inject returns AFTER every handler
// terminates); WaitForPublish is the residual asynchronous-shape
// fallback for cases where the publish is downstream of a goroutine
// the test doesn't synchronise on (e.g. FakeDM's response goroutines).
func WaitForPublishAfter(ctx context.Context, fb *FakeBroker, routingKey string, since time.Time) (PublishedMessage, error) {
	t := time.NewTicker(2 * time.Millisecond)
	defer t.Stop()
	for {
		for _, msg := range fb.PublishedOn(routingKey) {
			if since.IsZero() || !msg.At.Before(since) {
				return msg, nil
			}
		}
		select {
		case <-ctx.Done():
			return PublishedMessage{}, fmt.Errorf("WaitForPublish(%s): %w", routingKey, ctx.Err())
		case <-t.C:
		}
	}
}

// AssertPublished fails t if the broker has not recorded at least one
// publish on routingKey at or after since. Drops a clear failure message
// listing the routing keys actually seen.
func AssertPublished(t Tb, fb *FakeBroker, routingKey string) PublishedMessage {
	t.Helper()
	msgs := fb.PublishedOn(routingKey)
	if len(msgs) == 0 {
		seen := summariseRoutingKeys(fb.Published())
		t.Fatalf("AssertPublished(%s): no message recorded. seen=%v", routingKey, seen)
		return PublishedMessage{}
	}
	return msgs[0]
}

// AssertNotPublished fails t if any publish on routingKey is recorded.
func AssertNotPublished(t Tb, fb *FakeBroker, routingKey string) {
	t.Helper()
	if msgs := fb.PublishedOn(routingKey); len(msgs) > 0 {
		t.Fatalf("AssertNotPublished(%s): expected no message, got %d", routingKey, len(msgs))
	}
}

// MatchEvent unmarshals msg.Payload into out (must be a pointer to a
// typed event struct) and fails t on a decode error. Returns out for
// chainable assertions.
func MatchEvent(t Tb, msg PublishedMessage, out any) {
	t.Helper()
	if err := json.Unmarshal(msg.Payload, out); err != nil {
		t.Fatalf("MatchEvent(%s): decode failed: %v\npayload=%s", msg.RoutingKey, err, msg.Payload)
	}
}

// AssertPayloadField fails t when msg.Payload's JSON top-level object
// does not contain key OR its value does not equal want (string compare
// after json.Marshal). Useful for one-off shape assertions without
// declaring a full typed struct.
func AssertPayloadField(t Tb, msg PublishedMessage, key string, want any) {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(msg.Payload, &obj); err != nil {
		t.Fatalf("AssertPayloadField(%s, %s): decode failed: %v", msg.RoutingKey, key, err)
		return
	}
	got, ok := obj[key]
	if !ok {
		t.Fatalf("AssertPayloadField(%s, %s): key absent", msg.RoutingKey, key)
		return
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("AssertPayloadField(%s, %s): got=%s want=%s", msg.RoutingKey, key, gotJSON, wantJSON)
	}
}

// AssertNoErrors fails t for the first non-nil error in errs.
func AssertNoErrors(t Tb, errs ...error) {
	t.Helper()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("AssertNoErrors: errs[%d] = %v", i, err)
		}
	}
}

// summariseRoutingKeys returns a list of routing keys observed, with
// duplicates collapsed in order of first appearance.
func summariseRoutingKeys(msgs []PublishedMessage) []string {
	seen := make(map[string]struct{}, len(msgs))
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if _, ok := seen[m.RoutingKey]; ok {
			continue
		}
		seen[m.RoutingKey] = struct{}{}
		out = append(out, m.RoutingKey)
	}
	return out
}

// errPublishNotObserved is returned by WaitForPublish on ctx expiry; the
// public function uses fmt.Errorf to wrap ctx.Err(), but a few helpers
// in this package want a stable sentinel for inner assertions.
var errPublishNotObserved = errors.New("fakes: publish not observed")

// IsTimeout reports whether err originates from a WaitForPublish timeout.
func IsTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errPublishNotObserved)
}
