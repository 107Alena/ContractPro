package jobid

import (
	"regexp"
	"sync"
	"testing"
)

var uuidV4Regex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewJobID_MatchesUUIDv4Shape(t *testing.T) {
	id := NewJobID()
	if !uuidV4Regex.MatchString(id) {
		t.Fatalf("NewJobID() = %q, does not match UUID v4 regex", id)
	}
}

func TestNewJobID_ConcurrentUniqueness(t *testing.T) {
	const n = 100

	ids := make([]string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			ids[i] = NewJobID()
		}()
	}
	wg.Wait()

	seen := make(map[string]struct{}, n)
	for _, id := range ids {
		if !uuidV4Regex.MatchString(id) {
			t.Fatalf("invalid UUID v4 returned: %q", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate job_id generated: %q", id)
		}
		seen[id] = struct{}{}
	}

	if len(seen) != n {
		t.Fatalf("got %d unique IDs, want %d", len(seen), n)
	}
}
