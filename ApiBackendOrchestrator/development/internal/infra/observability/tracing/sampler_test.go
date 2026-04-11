package tracing

import (
	"strings"
	"testing"
)

func TestNewOrchSampler_ReturnsNonNil(t *testing.T) {
	s := NewOrchSampler()
	if s == nil {
		t.Fatal("expected non-nil sampler")
	}
}

func TestOrchSampler_Description(t *testing.T) {
	s := NewOrchSampler()
	desc := s.Description()
	if desc == "" {
		t.Error("expected non-empty sampler description")
	}
}

func TestOrchSampler_IsParentBased(t *testing.T) {
	s := NewOrchSampler()
	desc := s.Description()
	if !strings.Contains(desc, "ParentBased") {
		t.Errorf("expected 'ParentBased' in description, got: %s", desc)
	}
}

func TestOrchSampler_IncludesTraceIDRatio(t *testing.T) {
	s := NewOrchSampler()
	desc := s.Description()
	if !strings.Contains(desc, "TraceIDRatioBased") {
		t.Errorf("expected 'TraceIDRatioBased' in description, got: %s", desc)
	}
}
