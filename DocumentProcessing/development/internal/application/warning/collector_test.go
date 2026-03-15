package warning

import (
	"sync"
	"testing"

	"contractpro/document-processing/internal/domain/model"
)

func makeWarning(code, message string, stage model.ProcessingStage) model.ProcessingWarning {
	return model.ProcessingWarning{
		Code:    code,
		Message: message,
		Stage:   stage,
	}
}

func TestCollector_AddAndCollect(t *testing.T) {
	t.Run("single warning", func(t *testing.T) {
		c := NewCollector()
		w := makeWarning("LOW_CONFIDENCE", "page 3 low confidence", model.ProcessingStageOCR)

		c.Add(w)
		got := c.Collect()

		if len(got) != 1 {
			t.Fatalf("expected 1 warning, got %d", len(got))
		}
		if got[0] != w {
			t.Errorf("expected %+v, got %+v", w, got[0])
		}
	})

	t.Run("multiple warnings", func(t *testing.T) {
		c := NewCollector()
		warnings := []model.ProcessingWarning{
			makeWarning("LOW_CONFIDENCE", "page 1", model.ProcessingStageOCR),
			makeWarning("PARTIAL_TEXT", "section missing", model.ProcessingStageTextExtraction),
			makeWarning("AMBIGUOUS_HEADER", "header unclear", model.ProcessingStageStructureExtract),
		}

		for _, w := range warnings {
			c.Add(w)
		}
		got := c.Collect()

		if len(got) != len(warnings) {
			t.Fatalf("expected %d warnings, got %d", len(warnings), len(got))
		}
		for i, w := range warnings {
			if got[i] != w {
				t.Errorf("warning[%d]: expected %+v, got %+v", i, w, got[i])
			}
		}
	})
}

func TestCollector_HasWarnings(t *testing.T) {
	c := NewCollector()

	if c.HasWarnings() {
		t.Error("expected HasWarnings()=false on empty collector")
	}

	c.Add(makeWarning("W1", "msg", model.ProcessingStageOCR))

	if !c.HasWarnings() {
		t.Error("expected HasWarnings()=true after Add")
	}
}

func TestCollector_Reset(t *testing.T) {
	c := NewCollector()
	c.Add(makeWarning("W1", "msg1", model.ProcessingStageOCR))
	c.Add(makeWarning("W2", "msg2", model.ProcessingStageTextExtraction))

	c.Reset()

	if c.HasWarnings() {
		t.Error("expected HasWarnings()=false after Reset")
	}
	got := c.Collect()
	if got != nil {
		t.Errorf("expected nil after Reset, got %v", got)
	}
}

func TestCollector_CollectReturnsCopy(t *testing.T) {
	c := NewCollector()
	c.Add(makeWarning("W1", "original", model.ProcessingStageOCR))

	got := c.Collect()
	got[0].Message = "mutated"

	internal := c.Collect()
	if internal[0].Message != "original" {
		t.Errorf("Collect must return a copy; internal state was mutated to %q", internal[0].Message)
	}
}

func TestCollector_CollectEmptyReturnsNil(t *testing.T) {
	c := NewCollector()
	got := c.Collect()
	if got != nil {
		t.Errorf("expected nil for empty collector, got %v", got)
	}
}

func TestCollector_ConcurrentAdd(t *testing.T) {
	const goroutines = 100
	c := NewCollector()

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			c.Add(makeWarning("CONCURRENT", "msg", model.ProcessingStageOCR))
		}(i)
	}
	wg.Wait()

	got := c.Collect()
	if len(got) != goroutines {
		t.Errorf("expected %d warnings, got %d", goroutines, len(got))
	}
}

func TestCollector_ConcurrentAddAndRead(t *testing.T) {
	const goroutines = 100
	c := NewCollector()

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			c.Add(makeWarning("W", "msg", model.ProcessingStageOCR))
		}()
		go func() {
			defer wg.Done()
			_ = c.Collect()
		}()
		go func() {
			defer wg.Done()
			_ = c.HasWarnings()
		}()
	}
	wg.Wait()

	got := c.Collect()
	if len(got) != goroutines {
		t.Errorf("expected %d warnings, got %d", goroutines, len(got))
	}
}
