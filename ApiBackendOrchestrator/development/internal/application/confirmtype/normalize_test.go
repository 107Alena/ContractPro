package confirmtype

import (
	"errors"
	"testing"
)

// allRussianToEnglish lists every RU→EN pair for ASSUMPTION-LIC-16. Kept as
// a package-private slice so tests assert on the canonical order independent
// of map iteration randomness.
var allRussianToEnglish = []struct {
	russian string
	english string
}{
	{"услуги", "SERVICES"},
	{"поставка", "SUPPLY"},
	{"подряд", "WORK_CONTRACT"},
	{"аренда", "LEASE"},
	{"NDA", "NDA"},
	{"купля-продажа", "SALE"},
	{"лицензия", "LICENSE"},
	{"агентский", "AGENCY"},
	{"займ", "LOAN"},
	{"страхование", "INSURANCE"},
	{"трудовой", "EMPLOYMENT_CIVIL"},
	{"иное", "OTHER"},
}

func TestNormalizeContractType_AllRussianValues(t *testing.T) {
	if want := 12; len(allRussianToEnglish) != want {
		t.Fatalf("test fixture length = %d, want %d (ASSUMPTION-LIC-16 has 12 pairs)", len(allRussianToEnglish), want)
	}
	for _, tc := range allRussianToEnglish {
		t.Run(tc.russian, func(t *testing.T) {
			got, err := NormalizeContractType(tc.russian)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.russian, err)
			}
			if got != tc.english {
				t.Errorf("NormalizeContractType(%q) = %q, want %q", tc.russian, got, tc.english)
			}
		})
	}
}

func TestNormalizeContractType_AllEnglishPassthrough(t *testing.T) {
	for _, tc := range allRussianToEnglish {
		t.Run(tc.english, func(t *testing.T) {
			got, err := NormalizeContractType(tc.english)
			if err != nil {
				t.Fatalf("unexpected error for English enum %q: %v", tc.english, err)
			}
			if got != tc.english {
				t.Errorf("NormalizeContractType(%q) = %q, want %q (pass-through)", tc.english, got, tc.english)
			}
		})
	}
}

func TestNormalizeContractType_CaseInsensitiveRussian(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"услуги", "SERVICES"},
		{"Услуги", "SERVICES"},
		{"УСЛУГИ", "SERVICES"},
		{"УсЛуГи", "SERVICES"},
		{"Аренда", "LEASE"},
		{"АРЕНДА", "LEASE"},
		{"Купля-Продажа", "SALE"},
		{"КУПЛЯ-ПРОДАЖА", "SALE"},
		{"nda", "NDA"},
		{"Nda", "NDA"},
		{"Иное", "OTHER"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := NormalizeContractType(tc.input)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("NormalizeContractType(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeContractType_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"abracadabra", "абракадабра"},
		{"partial_match", "услуг"},
		{"trailing_garbage", "услугиX"},
		{"english_lowercase", "services"},
		{"english_mixed_case", "Services"},
		{"unknown_english", "unknown_enum"},
		{"unrelated_word", "договор"},
		{"random_unicode", "🤖"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeContractType(tc.input)
			if !errors.Is(err, ErrInvalidContractType) {
				t.Errorf("NormalizeContractType(%q) error = %v, want ErrInvalidContractType", tc.input, err)
			}
			if got != "" {
				t.Errorf("NormalizeContractType(%q) returned non-empty %q on error", tc.input, got)
			}
		})
	}
}

// TestNormalizeContractType_NDABothSides exercises the edge case where the
// English enum value ("NDA") matches the lowercased Russian key ("nda")
// after case-folding — confirming both lookup paths converge on the same
// canonical value without conflict.
func TestNormalizeContractType_NDABothSides(t *testing.T) {
	for _, in := range []string{"NDA", "nda", "Nda", "nDa"} {
		got, err := NormalizeContractType(in)
		if err != nil {
			t.Errorf("NormalizeContractType(%q) error = %v, want nil", in, err)
			continue
		}
		if got != "NDA" {
			t.Errorf("NormalizeContractType(%q) = %q, want NDA", in, got)
		}
	}
}

// TestNormalizeContractType_TrimsSurroundingWhitespace locks in the
// defense-in-depth contract that the function strips surrounding whitespace
// (callers that already TrimSpace are unaffected, but reuse from a different
// caller without trimming must still work).
func TestNormalizeContractType_TrimsSurroundingWhitespace(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{" услуги", "SERVICES"},
		{"услуги ", "SERVICES"},
		{"  услуги  ", "SERVICES"},
		{"\tуслуги\n", "SERVICES"},
		{" SERVICES ", "SERVICES"},
		{"\nNDA\t", "NDA"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := NormalizeContractType(tc.input)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("NormalizeContractType(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestNormalizeContractType_EnglishWhitelistDerivedFromRUMap verifies the
// invariant that the English whitelist is exactly the set of values from
// the RU→EN map — single source of truth, no drift possible.
func TestNormalizeContractType_EnglishWhitelistDerivedFromRUMap(t *testing.T) {
	if got, want := len(enWhitelist), len(ruToEN); got != want {
		t.Errorf("len(enWhitelist) = %d, want %d (must equal RU map cardinality)", got, want)
	}
	for _, en := range ruToEN {
		if _, ok := enWhitelist[en]; !ok {
			t.Errorf("EN value %q from ruToEN missing in enWhitelist", en)
		}
	}
}
