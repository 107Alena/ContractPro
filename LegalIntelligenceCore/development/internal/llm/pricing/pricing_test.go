package pricing

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// writeTemp writes content to a fresh file in t.TempDir and returns the path.
func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp %q: %v", name, err)
	}
	return p
}

func TestLoad_Valid_WithAndWithoutCached(t *testing.T) {
	// gemini omits cached → must default to 0.0 with CachedRateDefaulted=true.
	yml := `claude-sonnet-4-6:
  input_per_m_token_usd: 3.00
  cached_input_per_m_token_usd: 0.30
  output_per_m_token_usd: 15.00
gpt-4.1:
  input_per_m_token_usd: 2.50
  cached_input_per_m_token_usd: 1.25
  output_per_m_token_usd: 10.00
gemini-2.5-pro:
  input_per_m_token_usd: 1.25
  output_per_m_token_usd: 5.00
`
	tbl, err := Load(writeTemp(t, "pricing.yaml", yml))
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if len(tbl) != 3 {
		t.Fatalf("len(table) = %d, want 3", len(tbl))
	}

	claude := tbl["claude-sonnet-4-6"]
	if claude.InputPerMTokenUSD != 3.00 || claude.CachedInputPerMTokenUSD != 0.30 || claude.OutputPerMTokenUSD != 15.00 {
		t.Fatalf("claude pricing = %+v, want {3,0.30,15}", claude)
	}
	if claude.CachedRateDefaulted {
		t.Fatalf("claude.CachedRateDefaulted = true, want false (key present)")
	}

	gem := tbl["gemini-2.5-pro"]
	if gem.CachedInputPerMTokenUSD != 0.0 {
		t.Fatalf("gemini cached = %v, want 0.0 (key absent → explicit 0.0)", gem.CachedInputPerMTokenUSD)
	}
	if !gem.CachedRateDefaulted {
		t.Fatalf("gemini.CachedRateDefaulted = false, want true (MF-5: absent key observable)")
	}
}

func TestLoad_Errors(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"missing input", "m1:\n  output_per_m_token_usd: 5.0\n"},
		{"missing output", "m1:\n  input_per_m_token_usd: 5.0\n"},
		{"negative input", "m1:\n  input_per_m_token_usd: -1\n  output_per_m_token_usd: 5.0\n"},
		{"negative cached", "m1:\n  input_per_m_token_usd: 1\n  cached_input_per_m_token_usd: -0.1\n  output_per_m_token_usd: 5.0\n"},
		{"NaN input", "m1:\n  input_per_m_token_usd: .nan\n  output_per_m_token_usd: 5.0\n"},
		{"+Inf output", "m1:\n  input_per_m_token_usd: 1\n  output_per_m_token_usd: .inf\n"},
		{"-Inf cached", "m1:\n  input_per_m_token_usd: 1\n  cached_input_per_m_token_usd: -.inf\n  output_per_m_token_usd: 5.0\n"},
		{"no models", "\n"},
		{"empty mapping", "{}\n"},
		{"unknown key (typo, strict)", "m1:\n  imput_per_m_token_usd: 3\n  output_per_m_token_usd: 5\n"},
		{"malformed yaml", "m1: : :\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Load(writeTemp(t, "p.yaml", c.content)); err == nil {
				t.Fatalf("Load(%q) = nil error, want error", c.name)
			}
		})
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("Load(missing path) = nil error, want error")
	}
	if !os.IsNotExist(unwrapPathErr(err)) {
		t.Fatalf("error %v should wrap os.ErrNotExist", err)
	}
}

// unwrapPathErr digs out the underlying os error so the test can assert it
// is a not-exist error regardless of the fmt.Errorf %w wrapping.
func unwrapPathErr(err error) error {
	type unwrapper interface{ Unwrap() error }
	for err != nil {
		if pe, ok := err.(*os.PathError); ok {
			return pe
		}
		u, ok := err.(unwrapper)
		if !ok {
			return err
		}
		err = u.Unwrap()
	}
	return err
}

func TestCostUSD_Formula_AcceptanceStep2(t *testing.T) {
	// Acceptance step 2: claude-sonnet-4-6, input=10000, output=1000.
	// cost = (10000*3.00 + 0*0.30 + 1000*15.00)/1e6 = 45000/1e6 = 0.045 USD.
	tbl := Table{"claude-sonnet-4-6": {InputPerMTokenUSD: 3.00, CachedInputPerMTokenUSD: 0.30, OutputPerMTokenUSD: 15.00}}
	usd, known := tbl.CostUSD("claude-sonnet-4-6", 10_000, 0, 1_000)
	if !known {
		t.Fatal("known = false, want true")
	}
	if math.Abs(usd-0.045) > 1e-12 {
		t.Fatalf("cost = %v, want 0.045", usd)
	}
}

func TestCostUSD_CachedUsesCachedRate_AcceptanceStep3(t *testing.T) {
	// Acceptance step 3: cached tokens bill at CachedInputPerMTokenUSD
	// (0.30), NOT the input rate (3.00). 1,000,000 cached tokens →
	// $0.30 with the cached rate, $3.00 if it wrongly used the input rate.
	tbl := Table{"claude-sonnet-4-6": {InputPerMTokenUSD: 3.00, CachedInputPerMTokenUSD: 0.30, OutputPerMTokenUSD: 15.00}}
	usd, _ := tbl.CostUSD("claude-sonnet-4-6", 0, 1_000_000, 0)
	if math.Abs(usd-0.30) > 1e-12 {
		t.Fatalf("cached cost = %v, want 0.30 (cached rate, not input rate 3.00)", usd)
	}
}

func TestCostUSD_UnknownModel(t *testing.T) {
	tbl := Table{"claude-sonnet-4-6": {InputPerMTokenUSD: 3}}
	usd, known := tbl.CostUSD("gpt-9", 100, 0, 100)
	if known {
		t.Fatal("known = true for absent model, want false")
	}
	if usd != 0 {
		t.Fatalf("usd = %v for unknown model, want 0", usd)
	}
}

// TestCostUSD_LargeTokenCounts_NoOverflow pins MF-4: a token count whose
// product with the rate would overflow an int intermediate must still be
// exact via per-term float64 promotion. 2e9 input tokens × $3/M = $6000.
func TestCostUSD_LargeTokenCounts_NoOverflow(t *testing.T) {
	tbl := Table{"m": {InputPerMTokenUSD: 3.00, OutputPerMTokenUSD: 15.00}}
	usd, known := tbl.CostUSD("m", 2_000_000_000, 0, 0)
	if !known {
		t.Fatal("known = false, want true")
	}
	if math.Abs(usd-6000.0) > 1e-6 {
		t.Fatalf("cost = %v, want 6000.0 (per-term float64 promotion, no int overflow)", usd)
	}
}

func TestCostUSD_NegativeTokensClamped(t *testing.T) {
	tbl := Table{"m": {InputPerMTokenUSD: 3, CachedInputPerMTokenUSD: 1, OutputPerMTokenUSD: 5}}
	usd, known := tbl.CostUSD("m", -1000, -1000, -1000)
	if !known {
		t.Fatal("known = false, want true")
	}
	if usd != 0 {
		t.Fatalf("cost with all-negative tokens = %v, want 0 (clamped)", usd)
	}
}
