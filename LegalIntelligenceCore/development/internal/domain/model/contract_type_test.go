package model

import "testing"

func TestContractType_IsValid_AllWhitelisted(t *testing.T) {
	for _, ct := range AllContractTypes() {
		if !ct.IsValid() {
			t.Errorf("AllContractTypes returned %q but IsValid() = false", ct)
		}
	}
}

func TestContractType_IsValid_RejectsUnknown(t *testing.T) {
	rejected := []ContractType{
		"",
		"INVALID",
		"services",        // case-sensitivity
		"SERVICES ",       // trailing space
		"СЕРВИС",          // Cyrillic look-alike
		"EXOTIC_CONTRACT", // not in whitelist
	}
	for _, ct := range rejected {
		if ct.IsValid() {
			t.Errorf("%q must be invalid", ct)
		}
	}
}

func TestContractType_WhitelistCount(t *testing.T) {
	const expected = 12
	if got := len(AllContractTypes()); got != expected {
		t.Errorf("AllContractTypes count = %d, want %d (ai-agents-pipeline.md §1)", got, expected)
	}
}

func TestContractType_WireFormat_LockedConstants(t *testing.T) {
	// JSON Schema enums in event-catalog & agent schemas reference these
	// exact strings — drift would break upstream consumers.
	pairs := []struct {
		ct   ContractType
		wire string
	}{
		{ContractTypeServices, "SERVICES"},
		{ContractTypeSupply, "SUPPLY"},
		{ContractTypeWorkContract, "WORK_CONTRACT"},
		{ContractTypeLease, "LEASE"},
		{ContractTypeNDA, "NDA"},
		{ContractTypeSale, "SALE"},
		{ContractTypeLicense, "LICENSE"},
		{ContractTypeAgency, "AGENCY"},
		{ContractTypeLoan, "LOAN"},
		{ContractTypeInsurance, "INSURANCE"},
		{ContractTypeEmploymentCivil, "EMPLOYMENT_CIVIL"},
		{ContractTypeOther, "OTHER"},
	}
	for _, p := range pairs {
		if string(p.ct) != p.wire {
			t.Errorf("ContractType constant drift: got %q, want %q", string(p.ct), p.wire)
		}
	}
}

func TestValidateContractTypeFormat(t *testing.T) {
	cases := []struct {
		in     string
		wantOK bool
		reason string
	}{
		{"SERVICES", true, "valid wire format"},
		{"WORK_CONTRACT", true, "underscores allowed"},
		{"A", true, "single uppercase char fits 1..32"},
		{"", false, "empty string rejected (min length 1)"},
		{"services", false, "lowercase rejected"},
		{"SERVICES1", false, "digits rejected by ^[A-Z_]+$"},
		{"SERVICES-X", false, "hyphen rejected"},
		{"SERVICES ", false, "trailing space rejected"},
		{"VERY_LONG_NAME_THAT_EXCEEDS_THIRTY_TWO_CHARS", false, "33+ chars rejected"},
		{"SERVICESEXOTICTYPENAME1234567890123456", false, "long ASCII rejected"},
		{"СЕРВИС", false, "Cyrillic rejected"},
	}
	for _, tc := range cases {
		if got := ValidateContractTypeFormat(tc.in); got != tc.wantOK {
			t.Errorf("ValidateContractTypeFormat(%q) = %v, want %v (%s)", tc.in, got, tc.wantOK, tc.reason)
		}
	}
}

func TestIsValidContractType_CombinesFormatAndWhitelist(t *testing.T) {
	cases := []struct {
		in     string
		wantOK bool
	}{
		{"SERVICES", true},      // format ok + whitelist ok
		{"EXOTIC", false},       // format ok, NOT whitelisted
		{"services", false},     // format fails
		{"WORK_CONTRACT", true}, // format ok + whitelist ok
		{"", false},
	}
	for _, tc := range cases {
		if got := IsValidContractType(tc.in); got != tc.wantOK {
			t.Errorf("IsValidContractType(%q) = %v, want %v", tc.in, got, tc.wantOK)
		}
	}
}

func TestAllContractTypes_FreshSlice(t *testing.T) {
	a := AllContractTypes()
	a[0] = "MUTATED"
	b := AllContractTypes()
	if b[0] == "MUTATED" {
		t.Error("AllContractTypes must return a fresh slice on each call")
	}
}

func TestAllContractTypes_NoDuplicates(t *testing.T) {
	seen := make(map[ContractType]struct{})
	for _, ct := range AllContractTypes() {
		if _, dup := seen[ct]; dup {
			t.Errorf("AllContractTypes contains duplicate %q", ct)
		}
		seen[ct] = struct{}{}
	}
}
