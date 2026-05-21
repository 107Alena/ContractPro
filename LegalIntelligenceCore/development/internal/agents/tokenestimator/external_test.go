package tokenestimator_test

import (
	"strings"
	"testing"

	"contractpro/legal-intelligence-core/internal/agents/base"
	"contractpro/legal-intelligence-core/internal/agents/tokenestimator"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Compile-time assertion: *tokenestimator.Estimator implements the
// base.TokenEstimator seam (base/seams.go:135-137). This is the load-bearing
// wiring pin for LIC-TASK-047 — the wiring task does
// `base.Deps.Estimator = tokenestimator.NewEstimator(...)`, and a signature
// drift on either side breaks compilation here, not at production startup.
//
// This file is the SOLE allowed importer of internal/agents/base from this
// package. Because TestHermeticImports scans ONLY non-test files, it does
// not flag this _test.go import.
var _ base.TokenEstimator = (*tokenestimator.Estimator)(nil)

// TestFit_SeamContract_BlackBox is the smoke test that proves the wiring
// contract holds end-to-end: a freshly-constructed *Estimator handed
// in as a base.TokenEstimator yields the same (est, overBudget) the
// concrete type does.
func TestFit_SeamContract_BlackBox(t *testing.T) {
	e, err := tokenestimator.NewEstimator(tokenestimator.Config{
		MaxInputTokens:      150,
		MaxAgentInputTokens: 5,
		MaxIngestedBytes:    1024,
		CharsPerToken:       tokenestimator.DefaultCharsPerToken,
	})
	if err != nil {
		t.Fatalf("NewEstimator: %v", err)
	}
	var seam base.TokenEstimator = e // up-cast to the seam.

	req := port.CompletionRequest{
		System: strings.Repeat("a", 100), // ⌈100/3.5⌉ = 29 tokens > 5
	}
	est, over := seam.Fit(req)
	if est <= 5 {
		t.Errorf("Fit() via base.TokenEstimator: est = %d, want > 5", est)
	}
	if !over {
		t.Errorf("Fit() via base.TokenEstimator: overBudget = false, want true")
	}

	// Direct call must produce the SAME result (seam adds no behaviour).
	estDirect, overDirect := e.Fit(req)
	if est != estDirect || over != overDirect {
		t.Errorf("seam call diverged from direct: seam=(%d,%v) direct=(%d,%v)",
			est, over, estDirect, overDirect)
	}
}
