package metrics

import (
	"reflect"
	"sort"
	"testing"
)

// TestBuckets_SpecValues — locks the exact bucket values from
// observability.md §3. Changing a bucket boundary is a deliberate
// dashboard-impact decision; this test enforces it.
func TestBuckets_SpecValues(t *testing.T) {
	cases := []struct {
		name string
		got  []float64
		want []float64
	}{
		{
			name: "pipelineDurationBuckets §3.2",
			got:  pipelineDurationBuckets(),
			want: []float64{1, 5, 10, 15, 20, 30, 45, 60, 90, 120},
		},
		{
			name: "pipelineStageDurationBuckets §3.2",
			got:  pipelineStageDurationBuckets(),
			want: []float64{0.5, 1, 2, 5, 8, 12, 20, 30},
		},
		{
			name: "agentDurationBuckets §3.3",
			got:  agentDurationBuckets(),
			want: []float64{0.5, 1, 2, 5, 8, 12, 20},
		},
		{
			name: "agentInputTokensBuckets §3.3",
			got:  agentInputTokensBuckets(),
			want: []float64{1_000, 4_000, 8_000, 16_000, 32_000, 64_000},
		},
		{
			name: "agentOutputTokensBuckets §3.3",
			got:  agentOutputTokensBuckets(),
			want: []float64{100, 500, 1_000, 2_000, 4_000, 8_000},
		},
	}

	for _, tc := range cases {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// TestBuckets_MonotonicallyIncreasing — Prometheus requires histogram
// buckets to be strictly increasing; a regression here makes the
// histogram useless and is caught only at registration time.
func TestBuckets_MonotonicallyIncreasing(t *testing.T) {
	all := map[string][]float64{
		"pipelineDuration":      pipelineDurationBuckets(),
		"pipelineStageDuration": pipelineStageDurationBuckets(),
		"agentDuration":         agentDurationBuckets(),
		"agentInputTokens":      agentInputTokensBuckets(),
		"agentOutputTokens":     agentOutputTokensBuckets(),
		"llmLatency":            llmLatencyBuckets(),
		"dmRequestDuration":     dmRequestDurationBuckets(),
		"artifactsSizeBytes":    artifactsSizeBytesBuckets(),
	}
	for name, buckets := range all {
		if !sort.Float64sAreSorted(buckets) {
			t.Errorf("%s buckets are not sorted: %v", name, buckets)
		}
		for i := 1; i < len(buckets); i++ {
			if buckets[i] <= buckets[i-1] {
				t.Errorf("%s buckets not strictly increasing at index %d: %v", name, i, buckets)
				break
			}
		}
	}
}

// TestBuckets_AreFreshInstances — bucket constructors must return a
// fresh slice each call, so a misbehaving caller mutating the returned
// slice cannot corrupt subsequent registrations. (Pure-value invariant;
// caught by the assertion that two consecutive calls yield distinct
// backing arrays.)
func TestBuckets_AreFreshInstances(t *testing.T) {
	a := pipelineDurationBuckets()
	b := pipelineDurationBuckets()

	if len(a) == 0 {
		t.Fatal("pipelineDurationBuckets returned empty")
	}

	a[0] = 9999 // mutate caller-visible slice
	if b[0] == 9999 {
		t.Error("pipelineDurationBuckets returns aliased slice; caller mutation leaked into a second call")
	}
}
