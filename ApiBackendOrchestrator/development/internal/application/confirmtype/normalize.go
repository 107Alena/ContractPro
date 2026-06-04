package confirmtype

import (
	"errors"
	"sort"
	"strings"
)

// ErrInvalidContractType is returned by NormalizeContractType when the input
// matches neither a Russian UI label nor an English LIC enum value from the
// 12-pair whitelist (ASSUMPTION-LIC-16). The handler maps this sentinel to
// HTTP 400 with error_code INVALID_CONTRACT_TYPE.
var ErrInvalidContractType = errors.New("invalid contract type")

// ruToEN is the canonical 12-pair Russian → English contract-type mapping
// (ASSUMPTION-LIC-16). Russian keys are stored lowercased; lookups must
// lowercase the input first.
var ruToEN = map[string]string{
	"услуги":         "SERVICES",
	"поставка":       "SUPPLY",
	"подряд":         "WORK_CONTRACT",
	"аренда":         "LEASE",
	"nda":            "NDA",
	"купля-продажа":  "SALE",
	"лицензия":       "LICENSE",
	"агентский":      "AGENCY",
	"займ":           "LOAN",
	"страхование":    "INSURANCE",
	"трудовой":       "EMPLOYMENT_CIVIL",
	"иное":           "OTHER",
}

// enWhitelist is the set of 12 valid English LIC enum values, derived from
// ruToEN at package init to keep a single source of truth. Lookups are
// case-sensitive: only exact UPPER_SNAKE_CASE matches pass through.
var enWhitelist = func() map[string]struct{} {
	out := make(map[string]struct{}, len(ruToEN))
	for _, v := range ruToEN {
		out[v] = struct{}{}
	}
	return out
}()

// IsValidEnum reports whether s is one of the 12 canonical English LIC
// contract-type enum values (ASSUMPTION-LIC-16), matched case-sensitively in
// UPPER_SNAKE_CASE. It is the single source of truth for the enum whitelist,
// reused by other packages (e.g. the contracts list filter) to avoid drift.
func IsValidEnum(s string) bool {
	_, ok := enWhitelist[s]
	return ok
}

// EnumValues returns the 12 canonical English LIC contract-type enum values,
// sorted, for use in validation error messages.
func EnumValues() []string {
	out := make([]string, 0, len(enWhitelist))
	for v := range enWhitelist {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// NormalizeContractType maps the user-supplied contract_type to the canonical
// English LIC enum value (ASSUMPTION-LIC-16, ASSUMPTION-ORCH-16). It accepts:
//   - a Russian UI label, case-insensitive on the Russian key (e.g. "Услуги",
//     "УСЛУГИ", "услуги" all return "SERVICES");
//   - an English LIC enum value already in the 12-value whitelist, returned
//     as-is for backward-compatibility with API clients that pass the enum
//     directly (case-sensitive: "SERVICES" passes through, "services" does not).
//
// Surrounding whitespace is stripped from input as defense in depth — callers
// that already TrimSpace are unaffected. Empty, partial, unknown, or
// wrong-case English inputs return ErrInvalidContractType.
func NormalizeContractType(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ErrInvalidContractType
	}
	if v, ok := ruToEN[strings.ToLower(input)]; ok {
		return v, nil
	}
	if _, ok := enWhitelist[input]; ok {
		return input, nil
	}
	return "", ErrInvalidContractType
}
