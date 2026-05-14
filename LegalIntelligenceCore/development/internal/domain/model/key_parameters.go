package model

// KeyParameters is the output of Agent 2 — Key Parameters Extractor
// (ai-agents-pipeline.md §2). The FROZEN DM-side surface lives on the top-level
// fields. Result Aggregator drops InternalExtras and PromptInjectionDetected
// before publishing to DM (high-architecture.md §6.11 step 5).
type KeyParameters struct {
	Parties                 []string                      `json:"parties"`
	Subject                 string                        `json:"subject"`
	Price                   *string                       `json:"price"`
	Duration                *string                       `json:"duration"`
	Penalties               *string                       `json:"penalties"`
	Jurisdiction            *string                       `json:"jurisdiction"`
	InternalExtras          *KeyParametersInternalExtras  `json:"internal_extras,omitempty"`
	PromptInjectionDetected bool                          `json:"prompt_injection_detected"`
}

// KeyParametersInternalExtras is the LIC-internal extension carrying details
// used by downstream agents (3, 4, 8). NOT part of the DM-side contract — it
// is stripped by Result Aggregator before LegalAnalysisArtifactsReady is published.
type KeyParametersInternalExtras struct {
	ApplicableLaw       *string     `json:"applicable_law"`
	Termination         *string     `json:"termination"`
	AcceptanceProcedure *string     `json:"acceptance_procedure"`
	PartyRoles          []PartyRole `json:"party_roles,omitempty"`
	KeyDates            []KeyDate   `json:"key_dates,omitempty"`
}

// PartyRoleType narrows the role enum used by Agent 2 for party_roles[].role.
// Free-form party strings still belong on KeyParameters.Parties.
type PartyRoleType string

const (
	PartyRoleCustomer   PartyRoleType = "customer"
	PartyRoleContractor PartyRoleType = "contractor"
	PartyRoleSeller     PartyRoleType = "seller"
	PartyRoleBuyer      PartyRoleType = "buyer"
	PartyRoleLessor     PartyRoleType = "lessor"
	PartyRoleLessee     PartyRoleType = "lessee"
	PartyRoleLicensor   PartyRoleType = "licensor"
	PartyRoleLicensee   PartyRoleType = "licensee"
	PartyRoleParty      PartyRoleType = "party"
)

// IsValid reports whether r is a known PartyRoleType value.
func (r PartyRoleType) IsValid() bool {
	switch r {
	case PartyRoleCustomer, PartyRoleContractor, PartyRoleSeller, PartyRoleBuyer,
		PartyRoleLessor, PartyRoleLessee, PartyRoleLicensor, PartyRoleLicensee,
		PartyRoleParty:
		return true
	default:
		return false
	}
}

// PartyRole captures a single party identified by Agent 2 with its raw
// registry details (INN, OGRN). Checksums are NOT validated here; that
// is Agent 3's responsibility (Pre-LLM step in the Prompt Builder).
// All optional fields are wire-nullable per ai-agents-pipeline.md §2 schema
// (`type: ["string", "null"]`) — they must serialise as null when unset, not be
// omitted. Hence no `omitempty` on the *string fields.
type PartyRole struct {
	Name               string        `json:"name"`
	Role               PartyRoleType `json:"role"`
	INN                *string       `json:"inn"`
	OGRN               *string       `json:"ogrn"`
	Address            *string       `json:"address"`
	Signatory          *string       `json:"signatory"`
	SignatoryAuthority *string       `json:"signatory_authority"`
	ClauseRef          *string       `json:"clause_ref"`
}

// KeyDate is a date-bearing parameter found in the contract, tied to a
// specific clause via ClauseRef.
type KeyDate struct {
	Label     string `json:"label"`
	Value     string `json:"value"`
	ClauseRef string `json:"clause_ref"`
}
