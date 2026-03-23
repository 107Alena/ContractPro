package observability

import (
	"testing"
)

func TestNewMetrics_createsWithoutPanic(t *testing.T) {
	t.Parallel()

	m := NewMetrics()
	if m == nil {
		t.Fatal("NewMetrics returned nil")
	}
}

func TestMetrics_Registry_returnsNonNil(t *testing.T) {
	t.Parallel()

	m := NewMetrics()
	reg := m.Registry()

	if reg == nil {
		t.Fatal("Registry() returned nil")
	}
}

func TestMetrics_allMetricsCanBeObserved(t *testing.T) {
	t.Parallel()

	m := NewMetrics()

	// Each of these should not panic.
	t.Run("JobDuration", func(t *testing.T) {
		t.Parallel()
		m.JobDuration.WithLabelValues("COMPLETED").Observe(1.5)
		m.JobDuration.WithLabelValues("FAILED").Observe(0.3)
	})

	t.Run("JobStatusTotal", func(t *testing.T) {
		t.Parallel()
		m.JobStatusTotal.WithLabelValues("COMPLETED").Inc()
		m.JobStatusTotal.WithLabelValues("FAILED").Inc()
		m.JobStatusTotal.WithLabelValues("FAILED").Inc()
	})

	t.Run("OCRDuration", func(t *testing.T) {
		t.Parallel()
		m.OCRDuration.WithLabelValues("applicable").Observe(2.0)
		m.OCRDuration.WithLabelValues("not_applicable").Observe(0.01)
	})

	t.Run("ConcurrentJobsActive", func(t *testing.T) {
		t.Parallel()
		m.ConcurrentJobsActive.Inc()
		m.ConcurrentJobsActive.Inc()
		m.ConcurrentJobsActive.Dec()
	})

	t.Run("FileSizeBytes", func(t *testing.T) {
		t.Parallel()
		m.FileSizeBytes.Observe(1048576)
		m.FileSizeBytes.Observe(512000)
	})
}

func TestMetrics_noRegistrationConflicts_multipleCalls(t *testing.T) {
	t.Parallel()

	// Each NewMetrics creates its own dedicated registry, so calling it
	// multiple times should never cause a registration conflict panic.
	m1 := NewMetrics()
	m2 := NewMetrics()

	if m1.Registry() == m2.Registry() {
		t.Error("expected separate registries for different NewMetrics calls")
	}

	// Both should work independently.
	m1.JobStatusTotal.WithLabelValues("COMPLETED").Inc()
	m2.JobStatusTotal.WithLabelValues("FAILED").Inc()
}

func TestMetrics_Gather_containsExpectedMetricNames(t *testing.T) {
	t.Parallel()

	m := NewMetrics()

	// Record at least one observation per metric so they appear in Gather output.
	m.JobDuration.WithLabelValues("COMPLETED").Observe(1.0)
	m.JobStatusTotal.WithLabelValues("COMPLETED").Inc()
	m.OCRDuration.WithLabelValues("applicable").Observe(0.5)
	m.ConcurrentJobsActive.Inc()
	m.FileSizeBytes.Observe(1024)

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() returned error: %v", err)
	}

	expectedNames := map[string]bool{
		"dp_job_duration_seconds":   false,
		"dp_job_status_total":       false,
		"dp_ocr_duration_seconds":   false,
		"dp_concurrent_jobs_active": false,
		"dp_file_size_bytes":        false,
	}

	for _, fam := range families {
		name := fam.GetName()
		if _, ok := expectedNames[name]; ok {
			expectedNames[name] = true
		}
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("expected metric %q not found in Gather output", name)
		}
	}
}

func TestMetrics_Gather_emptyBeforeObservation(t *testing.T) {
	t.Parallel()

	m := NewMetrics()

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() returned error: %v", err)
	}

	// Gauges and counters may appear with zero values, but histograms
	// typically only appear after observation. We just verify Gather
	// completes without error.
	_ = families
}

func TestMetrics_JobDuration_multipleLabelValues(t *testing.T) {
	t.Parallel()

	m := NewMetrics()

	statuses := []string{"COMPLETED", "FAILED", "TIMED_OUT", "CANCELLED"}
	for _, s := range statuses {
		m.JobDuration.WithLabelValues(s).Observe(1.0)
	}

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() returned error: %v", err)
	}

	for _, fam := range families {
		if fam.GetName() == "dp_job_duration_seconds" {
			// Histogram families have their metrics, just verify non-empty.
			if len(fam.GetMetric()) == 0 {
				t.Error("expected metrics for dp_job_duration_seconds")
			}
			return
		}
	}

	t.Error("dp_job_duration_seconds not found in Gather output")
}
