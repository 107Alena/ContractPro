package warning

import (
	"sync"

	"contractpro/document-processing/internal/domain/model"
)

// Collector aggregates ProcessingWarning instances from pipeline steps
// in a thread-safe manner.
type Collector struct {
	mu       sync.Mutex
	warnings []model.ProcessingWarning
}

// NewCollector creates an empty Collector ready to use.
func NewCollector() *Collector {
	return &Collector{}
}

// Add appends a warning to the collector. Safe for concurrent use.
func (c *Collector) Add(w model.ProcessingWarning) {
	c.mu.Lock()
	c.warnings = append(c.warnings, w)
	c.mu.Unlock()
}

// Collect returns a copy of all collected warnings.
func (c *Collector) Collect() []model.ProcessingWarning {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.warnings) == 0 {
		return nil
	}
	out := make([]model.ProcessingWarning, len(c.warnings))
	copy(out, c.warnings)
	return out
}

// Reset clears all collected warnings.
func (c *Collector) Reset() {
	c.mu.Lock()
	c.warnings = nil
	c.mu.Unlock()
}

// HasWarnings reports whether any warnings have been collected.
func (c *Collector) HasWarnings() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.warnings) > 0
}
