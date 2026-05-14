package model

import "regexp"

// ContractType is the legal classification of a contract under Russian civil
// law (12-value whitelist per ai-agents-pipeline.md §1 and ASSUMPTION-LIC-16
// in high-architecture.md).
type ContractType string

const (
	ContractTypeServices        ContractType = "SERVICES"          // гл. 39 ГК РФ — возмездное оказание услуг
	ContractTypeSupply          ContractType = "SUPPLY"            // § 3 гл. 30 ГК РФ — поставка
	ContractTypeWorkContract    ContractType = "WORK_CONTRACT"     // гл. 37 ГК РФ — подряд
	ContractTypeLease           ContractType = "LEASE"             // гл. 34 ГК РФ — аренда
	ContractTypeNDA             ContractType = "NDA"               // 98-ФЗ + ст. 1465 ГК РФ — конфиденциальность
	ContractTypeSale            ContractType = "SALE"              // гл. 30 ГК РФ — купля-продажа (кроме поставки)
	ContractTypeLicense         ContractType = "LICENSE"           // ст. 1235 ГК РФ — лицензионный
	ContractTypeAgency          ContractType = "AGENCY"            // гл. 52 ГК РФ — агентирование
	ContractTypeLoan            ContractType = "LOAN"              // гл. 42 ГК РФ — заём / кредит
	ContractTypeInsurance       ContractType = "INSURANCE"         // гл. 48 ГК РФ — страхование
	ContractTypeEmploymentCivil ContractType = "EMPLOYMENT_CIVIL"  // гражданско-правовой с физлицом
	ContractTypeOther           ContractType = "OTHER"             // смешанный / иной
)

// contractTypeFormatRE enforces the wire-format constraint declared in
// integration-contracts.md and event-catalog.md: contract_type values are
// uppercase ASCII with underscores, 1..32 chars. The pattern protects the
// validator against accidental Cyrillic / lower-case drift in upstream
// payloads (orch.commands.user-confirmed-type, classification-uncertain).
var contractTypeFormatRE = regexp.MustCompile(`^[A-Z_]{1,32}$`)

var contractTypeSet map[ContractType]struct{}

// String returns the wire representation of the contract type.
func (t ContractType) String() string { return string(t) }

// IsValid reports whether t is one of the 12 whitelisted contract types.
// The check is O(1) and case-sensitive — values not in the whitelist (including
// case variations and Cyrillic look-alikes) are rejected.
func (t ContractType) IsValid() bool {
	_, ok := contractTypeSet[t]
	return ok
}

// ValidateContractTypeFormat reports whether s matches the wire-format regex
// ^[A-Z_]{1,32}$ used by event schemas. It does NOT check whitelist
// membership — that is the contract of IsValid / IsValidContractType. Callers
// that receive contract_type from external sources (user-confirmed-type)
// MUST run both checks (see error-handling.md §3.6 INVALID_CONTRACT_TYPE).
func ValidateContractTypeFormat(s string) bool {
	return contractTypeFormatRE.MatchString(s)
}

// IsValidContractType is the combined gate: wire-format check then whitelist
// check. Both must pass.
func IsValidContractType(s string) bool {
	if !contractTypeFormatRE.MatchString(s) {
		return false
	}
	_, ok := contractTypeSet[ContractType(s)]
	return ok
}

// AllContractTypes returns a fresh slice with every whitelisted ContractType
// in declaration order. Callers may mutate the returned slice.
func AllContractTypes() []ContractType {
	return []ContractType{
		ContractTypeServices,
		ContractTypeSupply,
		ContractTypeWorkContract,
		ContractTypeLease,
		ContractTypeNDA,
		ContractTypeSale,
		ContractTypeLicense,
		ContractTypeAgency,
		ContractTypeLoan,
		ContractTypeInsurance,
		ContractTypeEmploymentCivil,
		ContractTypeOther,
	}
}

func init() {
	all := AllContractTypes()
	contractTypeSet = make(map[ContractType]struct{}, len(all))
	for _, t := range all {
		contractTypeSet[t] = struct{}{}
	}
}
