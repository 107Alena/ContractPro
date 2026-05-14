package metrics

// Histogram bucket sets defined by observability.md §3.
// Bucket slices are returned by constructor functions rather than declared
// as package-level vars — slices in Go cannot be const, and an exported
// var can be silently mutated by a misbehaving caller. Returning a freshly
// allocated slice on every call is the cheapest way to guarantee
// immutability; this happens once at process start, not on every metric
// observation, so the allocation cost is irrelevant.

// pipelineDurationBuckets — observability.md §3.2.
// Range: 1s..120s (matches LIC_JOB_TIMEOUT=90s + headroom).
func pipelineDurationBuckets() []float64 {
	return []float64{1, 5, 10, 15, 20, 30, 45, 60, 90, 120}
}

// pipelineStageDurationBuckets — observability.md §3.2 (lic_pipeline_stage_duration_seconds).
// Stage timeouts vary 5..20s per agent; bucket range matches per-agent histogram.
func pipelineStageDurationBuckets() []float64 {
	return []float64{0.5, 1, 2, 5, 8, 12, 20, 30}
}

// agentDurationBuckets — observability.md §3.3.
func agentDurationBuckets() []float64 {
	return []float64{0.5, 1, 2, 5, 8, 12, 20}
}

// agentInputTokensBuckets — observability.md §3.3 (1k..64k).
func agentInputTokensBuckets() []float64 {
	return []float64{1_000, 4_000, 8_000, 16_000, 32_000, 64_000}
}

// agentOutputTokensBuckets — observability.md §3.3 (100..8k).
func agentOutputTokensBuckets() []float64 {
	return []float64{100, 500, 1_000, 2_000, 4_000, 8_000}
}

// llmLatencyBuckets — covers typical LLM call latency from prompt-cache
// hit (~200ms) to a long uncached Claude opus call (~30s).
func llmLatencyBuckets() []float64 {
	return []float64{0.2, 0.5, 1, 2, 5, 10, 20, 30}
}

// dmRequestDurationBuckets — DM round-trip via RabbitMQ;
// LIC_DM_REQUEST_TIMEOUT=30s is the spec hard ceiling.
func dmRequestDurationBuckets() []float64 {
	return []float64{0.1, 0.5, 1, 2, 5, 10, 30}
}

// artifactsSizeBytesBuckets — LIC_MAX_INGESTED_BYTES default is 10 MiB;
// buckets up to 32 MiB give headroom.
func artifactsSizeBytesBuckets() []float64 {
	return []float64{
		64 * 1024,        // 64 KiB
		256 * 1024,       // 256 KiB
		1 * 1024 * 1024,  // 1 MiB
		4 * 1024 * 1024,  // 4 MiB
		10 * 1024 * 1024, // 10 MiB (limit)
		16 * 1024 * 1024, // 16 MiB
		32 * 1024 * 1024, // 32 MiB
	}
}
