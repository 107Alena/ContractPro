package kvstore

import (
	"sync"

	"github.com/redis/go-redis/v9"
)

// Subscription represents an active Redis Pub/Sub subscription.
// It manages the background goroutine that delivers messages to the handler.
//
// The caller MUST call Close to release resources. If the caller forgets,
// the goroutine runs until the parent context is cancelled.
//
// The handler must not block for extended periods — it is called sequentially
// on a dedicated goroutine, and blocking delays both message delivery and Close.
type Subscription struct {
	pubsub   *redis.PubSub
	cancel   func()
	done     chan struct{} // closed when the delivery goroutine exits
	once     sync.Once
	closeErr error
}

// Close unsubscribes from the channel, stops the delivery goroutine,
// and releases resources. Safe to call multiple times; subsequent calls
// return the same error as the first.
// Blocks until the delivery goroutine has exited.
func (s *Subscription) Close() error {
	s.once.Do(func() {
		s.cancel()
		if s.pubsub != nil {
			s.closeErr = s.pubsub.Close()
		}
		<-s.done
	})
	return s.closeErr
}
