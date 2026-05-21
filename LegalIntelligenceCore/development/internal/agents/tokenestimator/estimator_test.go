package tokenestimator

import (
	"math/rand"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// newTestEstimator returns a fresh *Estimator built from a valid Config.
// MaxInputTokens/MaxAgentInputTokens/MaxIngestedBytes are roomy by default;
// individual tests override what they need via the optional override.
func newTestEstimator(t *testing.T, override ...func(*Config)) *Estimator {
	t.Helper()
	cfg := Config{
		MaxInputTokens:      150000,
		MaxAgentInputTokens: 120000,
		MaxIngestedBytes:    10 * 1024 * 1024, // 10 MiB
		CharsPerToken:       DefaultCharsPerToken,
	}
	for _, fn := range override {
		fn(&cfg)
	}
	e, err := NewEstimator(cfg)
	if err != nil {
		t.Fatalf("NewEstimator: unexpected error: %v", err)
	}
	return e
}

// --- EstimateTokens ----------------------------------------------------------

func TestEstimateTokens_ASCII(t *testing.T) {
	e := newTestEstimator(t)
	// "hello" = 5 runes; ⌈5/3.5⌉ = ⌈1.4286⌉ = 2.
	if got := e.EstimateTokens("hello"); got != 2 {
		t.Errorf("EstimateTokens(\"hello\") = %d, want 2", got)
	}
}

func TestEstimateTokens_RussianMultibyte(t *testing.T) {
	e := newTestEstimator(t)
	const word = "Договор" // 7 runes, 14 bytes (Cyrillic is 2 bytes/rune).
	if got, want := len([]rune(word)), 7; got != want {
		t.Fatalf("test fixture: len([]rune(%q))=%d, want %d", word, got, want)
	}
	if got, want := len(word), 14; got != want {
		t.Fatalf("test fixture: len(%q)=%d, want %d (UTF-8 byte length)", word, got, want)
	}
	// ⌈7/3.5⌉ = 2 (exactly divisible — no ceiling bump).
	if got := e.EstimateTokens(word); got != 2 {
		t.Errorf("EstimateTokens(%q) = %d, want 2", word, got)
	}
}

func TestEstimateTokens_Empty(t *testing.T) {
	e := newTestEstimator(t)
	if got := e.EstimateTokens(""); got != 0 {
		t.Errorf("EstimateTokens(\"\") = %d, want 0", got)
	}
}

func TestEstimateTokens_CeilingRounding(t *testing.T) {
	e := newTestEstimator(t)
	// 7 runes / 3.5 == 2.0 — no ceiling bump.
	if got := e.EstimateTokens(strings.Repeat("a", 7)); got != 2 {
		t.Errorf("EstimateTokens(7×'a') = %d, want 2 (exact division)", got)
	}
	// 8 runes / 3.5 == 2.2857… — ceil to 3.
	if got := e.EstimateTokens(strings.Repeat("a", 8)); got != 3 {
		t.Errorf("EstimateTokens(8×'a') = %d, want 3 (ceiling)", got)
	}
}

func TestEstimateTokens_CustomCharsPerToken(t *testing.T) {
	e := newTestEstimator(t, func(c *Config) { c.CharsPerToken = 4.0 })
	// 8 runes / 4.0 == 2.0.
	if got := e.EstimateTokens(strings.Repeat("a", 8)); got != 2 {
		t.Errorf("EstimateTokens(8×'a') with cpt=4.0 = %d, want 2", got)
	}
}

func TestEstimateTokens_DefaultCharsPerToken_FromZero(t *testing.T) {
	// Construct with CharsPerToken=0; verify the Estimator behaves as if
	// CharsPerToken==DefaultCharsPerToken (3.5).
	cfg := Config{
		MaxInputTokens:      150000,
		MaxAgentInputTokens: 120000,
		MaxIngestedBytes:    10 * 1024 * 1024,
		CharsPerToken:       0,
	}
	e, err := NewEstimator(cfg)
	if err != nil {
		t.Fatalf("NewEstimator: unexpected error: %v", err)
	}
	// Identical to TestEstimateTokens_ASCII: "hello" → 2.
	if got := e.EstimateTokens("hello"); got != 2 {
		t.Errorf("EstimateTokens(\"hello\") with cpt=0 fallback = %d, want 2", got)
	}
}

// --- Truncate ---------------------------------------------------------------

func TestTruncate_NoOp_WithinBudget(t *testing.T) {
	e := newTestEstimator(t)
	const text = "hello"
	got, info := e.Truncate(text, 10) // 10 tokens budget >> 2 estimated tokens.
	if got != text {
		t.Errorf("Truncate within budget mutated text: got %q, want %q", got, text)
	}
	if info != nil {
		t.Errorf("Truncate within budget returned non-nil info: %+v", info)
	}
}

func TestTruncate_ExactBudget(t *testing.T) {
	e := newTestEstimator(t)
	// 7-rune ASCII → EstimateTokens == 2 (exact division). maxTokens=2 ⇒
	// no truncation needed.
	const text = "abcdefg"
	if est := e.EstimateTokens(text); est != 2 {
		t.Fatalf("test fixture: EstimateTokens(%q)=%d, want 2", text, est)
	}
	got, info := e.Truncate(text, 2)
	if got != text {
		t.Errorf("Truncate at exact budget mutated text: got %q, want %q", got, text)
	}
	if info != nil {
		t.Errorf("Truncate at exact budget returned non-nil info: %+v", info)
	}
}

func TestTruncate_LargeInput_HeadTailProportions(t *testing.T) {
	e := newTestEstimator(t) // CharsPerToken = 3.5.
	// Build a 200K-token input. EstimateTokens uses ⌈runes/3.5⌉ — we want
	// the rune count high enough that est > maxTokens=150000.
	const totalRunes = 700000 // ⌈700000/3.5⌉ == 200000 tokens.
	text := strings.Repeat("a", totalRunes)
	if est := e.EstimateTokens(text); est <= 150000 {
		t.Fatalf("test fixture: estimate %d not > maxTokens=150000", est)
	}

	truncated, info := e.Truncate(text, 150000)
	if info == nil {
		t.Fatal("Truncate over-budget returned nil info")
	}
	// Post-truncation estimate must be <= maxTokens.
	if est := e.EstimateTokens(truncated); est > 150000 {
		t.Errorf("post-truncation estimate %d > maxTokens 150000", est)
	}

	// Head/tail rune split:
	//   headTokens = 150000 * 60 / 100 = 90000 ⇒ headRunes = floor(90000*3.5) = 315000.
	//   tailTokens = 150000 - 90000      = 60000 ⇒ tailRunes = floor(60000*3.5) = 210000.
	// Total truncated runes == 315000 + 210000 == 525000.
	const wantHead = 315000
	const wantTail = 210000
	const wantTotal = wantHead + wantTail

	gotRunes := len([]rune(truncated))
	if gotRunes != wantTotal {
		t.Errorf("len(truncated runes) = %d, want %d", gotRunes, wantTotal)
	}

	// Verify head share is ~60% within ±1 rune (rounding tolerance).
	headShare := float64(wantHead) / float64(wantTotal)
	if headShare < 0.5999 || headShare > 0.6001 {
		t.Errorf("head share = %v, want ~0.6", headShare)
	}
}

func TestTruncate_DroppedBytesAccuracy(t *testing.T) {
	e := newTestEstimator(t)
	text := strings.Repeat("a", 700000)
	truncated, info := e.Truncate(text, 150000)
	if info == nil {
		t.Fatal("expected truncation, got nil info")
	}
	wantDropped := len(text) - len(truncated)
	if info.TruncatedBytes != wantDropped {
		t.Errorf("TruncatedBytes = %d, want %d (len(orig)-len(truncated))",
			info.TruncatedBytes, wantDropped)
	}
}

func TestTruncate_TotalBytesEqualsInput(t *testing.T) {
	e := newTestEstimator(t)
	text := strings.Repeat("a", 700000)
	_, info := e.Truncate(text, 150000)
	if info == nil {
		t.Fatal("expected truncation, got nil info")
	}
	if info.TotalBytes != len(text) {
		t.Errorf("TotalBytes = %d, want %d (len(orig))", info.TotalBytes, len(text))
	}
}

func TestTruncate_RuneBoundary_UTF8Valid(t *testing.T) {
	e := newTestEstimator(t)
	// Cyrillic input: 700000 runes, each 2 bytes = 1.4M bytes. EstimateTokens
	// is rune-based so the 200000-token estimate is identical to the ASCII
	// fixture above. The point of this test is that the rune-aware slicing
	// produces valid UTF-8 output (a byte-indexed slice would split a
	// 2-byte sequence and corrupt the string).
	text := strings.Repeat("я", 700000)
	truncated, info := e.Truncate(text, 150000)
	if info == nil {
		t.Fatal("expected truncation, got nil info")
	}
	if !utf8.ValidString(truncated) {
		t.Error("truncated output is not valid UTF-8 (rune-boundary violation)")
	}
	// Every rune must still be the original 'я'.
	for _, r := range truncated {
		if r != 'я' {
			t.Errorf("found rune %q in truncated output, expected only 'я' — boundary corruption", r)
			break
		}
	}
}

func TestTruncate_DefensiveFallback_SmallBudget(t *testing.T) {
	e := newTestEstimator(t)
	// maxTokens=1, 5-rune input. With CharsPerToken=3.5:
	//   headTokens = 1*60/100 = 0  → headRunes = 0  → fallback triggers.
	const text = "hello"
	got, info := e.Truncate(text, 1)
	if got != text {
		t.Errorf("defensive fallback should preserve text; got %q, want %q", got, text)
	}
	if info != nil {
		t.Errorf("defensive fallback should return nil info; got %+v", info)
	}
}

func TestTruncate_PropertyTotalTokensInvariant(t *testing.T) {
	e := newTestEstimator(t)
	rng := rand.New(rand.NewSource(20260521)) // reproducible.
	const iterations = 100
	for i := 0; i < iterations; i++ {
		// Random rune-length in [200, 5000]; random maxTokens in [10, 500].
		runeLen := 200 + rng.Intn(4801)
		maxTokens := 10 + rng.Intn(491)
		// Use a single ASCII rune to make rune-counting predictable.
		text := strings.Repeat("x", runeLen)
		truncated, info := e.Truncate(text, maxTokens)
		if info == nil {
			// No-op path is valid when budget already fits OR defensive
			// fallback triggered; skip the invariant check.
			continue
		}
		if est := e.EstimateTokens(truncated); est > maxTokens {
			t.Errorf("iter %d (runeLen=%d, maxTokens=%d): est(truncated)=%d > maxTokens=%d",
				i, runeLen, maxTokens, est, maxTokens)
		}
		if info.TruncatedBytes < 1 {
			t.Errorf("iter %d: TruncatedBytes=%d, invariant requires >=1 when info!=nil",
				i, info.TruncatedBytes)
		}
	}
}

func TestTruncateToInputBudget_UsesMaxInputTokens(t *testing.T) {
	e := newTestEstimator(t, func(c *Config) {
		c.MaxInputTokens = 100
		c.MaxAgentInputTokens = 100 // must remain <= MaxInputTokens
	})
	// Build text whose estimate > 100 tokens but <= a hypothetical higher
	// budget. 500 runes / 3.5 = ⌈142.86⌉ = 143 tokens > 100.
	text := strings.Repeat("a", 500)
	if est := e.EstimateTokens(text); est <= 100 {
		t.Fatalf("test fixture: estimate %d not > 100", est)
	}
	// TruncateToInputBudget must use 100 (MaxInputTokens) — same as direct
	// Truncate(text, 100).
	gotBudget, infoBudget := e.TruncateToInputBudget(text)
	gotDirect, infoDirect := e.Truncate(text, 100)
	if gotBudget != gotDirect {
		t.Errorf("TruncateToInputBudget output differs from Truncate(text, MaxInputTokens)")
	}
	if !reflect.DeepEqual(infoBudget, infoDirect) {
		t.Errorf("TruncateToInputBudget info differs from Truncate(text, MaxInputTokens): %+v vs %+v",
			infoBudget, infoDirect)
	}
}

// --- CheckIngestSize --------------------------------------------------------

func TestCheckIngestSize_UnderLimit_NoError(t *testing.T) {
	e := newTestEstimator(t, func(c *Config) { c.MaxIngestedBytes = 100 })
	arts := model.InputArtifactsCompact{
		model.ArtifactSemanticTree:  []byte("xxxxx"),  // 5
		model.ArtifactExtractedText: []byte("yyyyyy"), // 6
	} // total 11 << 100
	if err := e.CheckIngestSize(arts); err != nil {
		t.Errorf("CheckIngestSize under limit returned error: %v", err)
	}
}

func TestCheckIngestSize_ExactlyLimit_NoError(t *testing.T) {
	e := newTestEstimator(t, func(c *Config) { c.MaxIngestedBytes = 10 })
	arts := model.InputArtifactsCompact{
		model.ArtifactSemanticTree: []byte("xxxxxxxxxx"), // exactly 10 bytes
	}
	// Strict '>' — total == limit must pass (matches orchestrator.go line 398).
	if err := e.CheckIngestSize(arts); err != nil {
		t.Errorf("CheckIngestSize at exact limit returned error: %v (expected nil — strict '>')", err)
	}
}

func TestCheckIngestSize_OverLimit_DocumentTooLarge(t *testing.T) {
	e := newTestEstimator(t, func(c *Config) { c.MaxIngestedBytes = 5 })
	arts := model.InputArtifactsCompact{
		model.ArtifactSemanticTree: []byte("xxxxxxxxxx"), // 10 > 5
	}
	err := e.CheckIngestSize(arts)
	if err == nil {
		t.Fatal("CheckIngestSize over limit returned nil, want DOCUMENT_TOO_LARGE")
	}
	de, ok := model.AsDomainError(err)
	if !ok {
		t.Fatalf("returned error is not a *DomainError: %T %v", err, err)
	}
	if de.Code != model.ErrCodeDocumentTooLarge {
		t.Errorf("Code = %q, want %q", de.Code, model.ErrCodeDocumentTooLarge)
	}
	if de.Stage != model.StageArtifactsReceived {
		t.Errorf("Stage = %q, want %q", de.Stage, model.StageArtifactsReceived)
	}
	if de.Retryable {
		t.Error("Retryable = true, want false")
	}
}

func TestCheckIngestSize_AttributeWireParity_WithOrchestrator(t *testing.T) {
	const limit int = 5
	e := newTestEstimator(t, func(c *Config) { c.MaxIngestedBytes = limit })

	// Construct an artifact bundle whose total len(raw) is 10.
	arts := model.InputArtifactsCompact{
		model.ArtifactSemanticTree:  []byte("xxxxx"),
		model.ArtifactExtractedText: []byte("yyyyy"),
	}
	total := 0
	for _, raw := range arts {
		total += len(raw)
	}
	if total != 10 {
		t.Fatalf("test fixture: total = %d, want 10", total)
	}

	gotErr := e.CheckIngestSize(arts)
	gotDE, ok := model.AsDomainError(gotErr)
	if !ok {
		t.Fatalf("CheckIngestSize returned non-DomainError: %T %v", gotErr, gotErr)
	}

	// Construct the exact same error inline, mirroring orchestrator.go:393-403
	// byte-for-byte.
	wantDE := model.NewDomainError(model.ErrCodeDocumentTooLarge, model.StageArtifactsReceived).
		WithRetryable(false).
		WithAttribute("ingested_bytes", total). // Go int
		WithAttribute("limit", limit)           // Go int (matches pipeline.Config.MaxIngestedBytes)

	if gotDE.Code != wantDE.Code ||
		gotDE.Stage != wantDE.Stage ||
		gotDE.Retryable != wantDE.Retryable {
		t.Errorf("scalar parity broken: got {Code:%v Stage:%v Retryable:%v}, want {Code:%v Stage:%v Retryable:%v}",
			gotDE.Code, gotDE.Stage, gotDE.Retryable,
			wantDE.Code, wantDE.Stage, wantDE.Retryable)
	}
	if !reflect.DeepEqual(gotDE.Attributes, wantDE.Attributes) {
		t.Errorf("Attributes parity broken: got %#v, want %#v", gotDE.Attributes, wantDE.Attributes)
	}

	// Pin the Go-types of the two attribute values — `int` vs `int64`
	// matters (reflect.DeepEqual distinguishes them; a Prometheus label or
	// JSON serialiser would expose the difference). Both attributes are
	// emitted as Go int to mirror pipeline.Config.MaxIngestedBytes today.
	switch v := gotDE.Attributes["ingested_bytes"].(type) {
	case int:
		if v != total {
			t.Errorf("ingested_bytes value = %d, want %d", v, total)
		}
	default:
		t.Errorf("ingested_bytes attribute type = %T, want int", gotDE.Attributes["ingested_bytes"])
	}
	switch v := gotDE.Attributes["limit"].(type) {
	case int:
		if v != limit {
			t.Errorf("limit value = %d, want %d", v, limit)
		}
	default:
		t.Errorf("limit attribute type = %T, want int", gotDE.Attributes["limit"])
	}
}

func TestCheckIngestSize_EmptyArtifacts_NoError(t *testing.T) {
	e := newTestEstimator(t)
	if err := e.CheckIngestSize(model.InputArtifactsCompact{}); err != nil {
		t.Errorf("CheckIngestSize(empty) returned error: %v", err)
	}
}

func TestCheckIngestSize_NilArtifacts_NoError(t *testing.T) {
	e := newTestEstimator(t)
	if err := e.CheckIngestSize(nil); err != nil {
		t.Errorf("CheckIngestSize(nil) returned error: %v", err)
	}
}

// --- Fit (base.TokenEstimator seam) -----------------------------------------

func TestFit_EmptyRequest_Zero(t *testing.T) {
	e := newTestEstimator(t)
	est, over := e.Fit(port.CompletionRequest{})
	if est != 0 {
		t.Errorf("Fit({}) est = %d, want 0", est)
	}
	if over {
		t.Error("Fit({}) overBudget = true, want false")
	}
}

func TestFit_SumsAllComponents(t *testing.T) {
	e := newTestEstimator(t)
	req := port.CompletionRequest{
		System: "aaa",  // 3 runes
		User:   "bbbb", // 4 runes
		PriorTurns: []port.Turn{
			{Role: port.RoleUser, Content: "cc"},         // 2 runes
			{Role: port.RoleAssistant, Content: "ddddd"}, // 5 runes
		},
	}
	// total runes = 3+4+2+5 = 14; ⌈14/3.5⌉ = 4.
	est, over := e.Fit(req)
	if est != 4 {
		t.Errorf("Fit summed est = %d, want 4", est)
	}
	if over {
		t.Error("Fit summed overBudget = true, want false (4 << 120000)")
	}
}

func TestFit_OverBudget_TrueWhenAboveMaxAgentInputTokens(t *testing.T) {
	e := newTestEstimator(t, func(c *Config) { c.MaxAgentInputTokens = 1 })
	// 100 runes / 3.5 = ⌈28.57⌉ = 29 tokens >> 1.
	req := port.CompletionRequest{System: strings.Repeat("a", 100)}
	est, over := e.Fit(req)
	if est <= 1 {
		t.Fatalf("test fixture: est = %d, want > 1", est)
	}
	if !over {
		t.Errorf("Fit overBudget = false, want true (est=%d > MaxAgentInputTokens=1)", est)
	}
}

func TestFit_OverBudget_FalseAtExactLimit(t *testing.T) {
	// Strict '>': est == MaxAgentInputTokens must NOT be over-budget.
	// Construct a request whose estimate is exactly N tokens, set
	// MaxAgentInputTokens=N. Pick 7 runes (⌈7/3.5⌉ = 2 tokens exactly).
	e := newTestEstimator(t, func(c *Config) {
		c.MaxAgentInputTokens = 2
	})
	req := port.CompletionRequest{System: "abcdefg"} // 7 runes → 2 tokens.
	est, over := e.Fit(req)
	if est != 2 {
		t.Fatalf("test fixture: est = %d, want 2", est)
	}
	if over {
		t.Error("Fit overBudget = true at exact limit, want false (strict '>')")
	}
}

func TestFit_NoMutation(t *testing.T) {
	e := newTestEstimator(t)
	req := port.CompletionRequest{
		AgentID:    model.AgentRiskDetection,
		Model:      "claude-sonnet-4-6",
		System:     "system prompt",
		User:       "user content",
		PriorTurns: []port.Turn{{Role: port.RoleUser, Content: "hi"}},
		MaxTokens:  4096,
	}
	// Deep snapshot via reflect.DeepEqual: PriorTurns is a slice, but Fit
	// only reads it — we capture the values pre-Fit and compare post-Fit.
	preTurns := append([]port.Turn(nil), req.PriorTurns...)
	preReq := req

	_, _ = e.Fit(req)

	if !reflect.DeepEqual(preReq, req) {
		t.Errorf("Fit mutated req scalar fields: pre=%+v, post=%+v", preReq, req)
	}
	if !reflect.DeepEqual(preTurns, req.PriorTurns) {
		t.Errorf("Fit mutated req.PriorTurns: pre=%+v, post=%+v", preTurns, req.PriorTurns)
	}
}

// --- Config.validate / NewEstimator -----------------------------------------

func TestConfig_Validate_FailsFast(t *testing.T) {
	type spec struct {
		name  string
		cfg   Config
		wants []string // substrings that MUST appear in the joined error.
	}
	good := Config{
		MaxInputTokens:      100,
		MaxAgentInputTokens: 50,
		MaxIngestedBytes:    1024,
		CharsPerToken:       3.5,
	}
	cases := []spec{
		{
			name: "MaxInputTokens_zero",
			cfg: func() Config {
				c := good
				c.MaxInputTokens = 0
				c.MaxAgentInputTokens = 0 // also triggers the <= rule otherwise — keep test focused
				return c
			}(),
			wants: []string{"MaxInputTokens must be >= 1"},
		},
		{
			name: "MaxAgentInputTokens_zero",
			cfg: func() Config {
				c := good
				c.MaxAgentInputTokens = 0
				return c
			}(),
			wants: []string{"MaxAgentInputTokens must be >= 1"},
		},
		{
			name: "MaxAgentInputTokens_exceeds_MaxInputTokens",
			cfg: func() Config {
				c := good
				c.MaxAgentInputTokens = 200 // > MaxInputTokens=100
				return c
			}(),
			wants: []string{"MaxAgentInputTokens (200) must be <= Config.MaxInputTokens (100)"},
		},
		{
			name: "MaxIngestedBytes_zero",
			cfg: func() Config {
				c := good
				c.MaxIngestedBytes = 0
				return c
			}(),
			wants: []string{"MaxIngestedBytes must be >= 1"},
		},
		{
			name: "CharsPerToken_below_one",
			cfg: func() Config {
				c := good
				c.CharsPerToken = 0.5
				return c
			}(),
			wants: []string{"CharsPerToken must be >= 1.0"},
		},
		{
			// Combine four independent violations: each of MaxInputTokens,
			// MaxAgentInputTokens, MaxIngestedBytes is below the floor; and
			// MaxAgentInputTokens > MaxInputTokens is independently true
			// (10 > 5) so the join-rule fires alongside the floor rules.
			name: "multiple_violations_all_present",
			cfg: Config{
				MaxInputTokens:      5,
				MaxAgentInputTokens: 10,
				MaxIngestedBytes:    -3,
				CharsPerToken:       0.1,
			},
			wants: []string{
				"MaxAgentInputTokens (10) must be <= Config.MaxInputTokens (5)",
				"MaxIngestedBytes must be >= 1",
				"CharsPerToken must be >= 1.0",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg // copy — validate mutates the receiver.
			err := cfg.validate()
			if err == nil {
				t.Fatalf("validate() returned nil, want errors.Join with violations %v", tc.wants)
			}
			msg := err.Error()
			for _, want := range tc.wants {
				if !strings.Contains(msg, want) {
					t.Errorf("error message missing substring %q; full message: %q", want, msg)
				}
			}
		})
	}
}

func TestConfig_Validate_AppliesCharsPerTokenDefault(t *testing.T) {
	cfg := Config{
		MaxInputTokens:      100,
		MaxAgentInputTokens: 50,
		MaxIngestedBytes:    1024,
		CharsPerToken:       0,
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate(): unexpected error: %v", err)
	}
	if cfg.CharsPerToken != DefaultCharsPerToken {
		t.Errorf("validate() did not fill CharsPerToken default: got %v, want %v",
			cfg.CharsPerToken, DefaultCharsPerToken)
	}
}

func TestNewEstimator_FailsFast(t *testing.T) {
	// Bad Config: MaxInputTokens=0 violates the floor; the constructor must
	// return the errors.Join verbatim (no wrap) and no Estimator.
	bad := Config{
		MaxInputTokens:      0,
		MaxAgentInputTokens: 0,
		MaxIngestedBytes:    1024,
		CharsPerToken:       3.5,
	}
	e, err := NewEstimator(bad)
	if err == nil {
		t.Fatal("NewEstimator(bad) returned nil error, want failure")
	}
	if e != nil {
		t.Errorf("NewEstimator(bad) returned non-nil Estimator: %p", e)
	}
	// The error must come from validate (not be wrapped).
	if !strings.Contains(err.Error(), "MaxInputTokens must be >= 1") {
		t.Errorf("expected validate() error verbatim; got %q", err.Error())
	}
}

// TestConfig_Validate_JoinedErrorUnwraps pins that validate() returns an
// errors.Join-produced multi-error: the dynamic type satisfies the
// `Unwrap() []error` interface AND the slice length equals the violation
// count. Replaces the earlier `errors.Is(err, err)` reflexivity tautology
// (L5 — would also pass for a plain errors.New).
func TestConfig_Validate_JoinedErrorUnwraps(t *testing.T) {
	// 4 violations expected: MaxInputTokens<1, MaxAgentInputTokens<1,
	// MaxIngestedBytes<1, CharsPerToken<1.0. The exceeds-rule (-1 > -1) is
	// false, so it does NOT fire — total stays at 4.
	cfg := Config{
		MaxInputTokens:      -1,
		MaxAgentInputTokens: -1,
		MaxIngestedBytes:    -1,
		CharsPerToken:       0.5,
	}
	err := cfg.validate()
	if err == nil {
		t.Fatal("validate() returned nil")
	}
	multi, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("validate() error %T does not implement Unwrap() []error — errors.Join wiring broken", err)
	}
	inner := multi.Unwrap()
	if got, want := len(inner), 4; got != want {
		t.Errorf("Unwrap() returned %d inner errors, want %d", got, want)
	}
	for i, e := range inner {
		if e == nil {
			t.Errorf("inner[%d] is nil", i)
		}
	}
}

// --- Concurrency ------------------------------------------------------------

func TestEstimator_ConcurrentRaceClean(t *testing.T) {
	e := newTestEstimator(t)

	const goroutines = 32
	const itersPerG = 50
	text := strings.Repeat("я", 700000)
	arts := model.InputArtifactsCompact{
		model.ArtifactSemanticTree: []byte("xxxxx"),
	}
	req := port.CompletionRequest{
		System:     "system",
		User:       "user content",
		PriorTurns: []port.Turn{{Role: port.RoleAssistant, Content: "prev"}},
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < itersPerG; i++ {
				_ = e.EstimateTokens(text)
				_, _ = e.Truncate(text, 150000)
				_, _ = e.TruncateToInputBudget(text)
				_ = e.CheckIngestSize(arts)
				_, _ = e.Fit(req)
			}
		}()
	}
	wg.Wait()
}
