package promptbuilder

// innogrn.go is the pure, side-effect-free FNS (ФНС) deterministic
// control-digit validation of Russian taxpayer (ИНН) and state-registration
// (ОГРН / ОГРНИП) identifiers — high-architecture.md §6.7.2.
//
// "Pure" is deliberate: the Prometheus metric and the <validation_facts> XML
// rendering live in builder.go, so these functions are independently
// table-testable (acceptance steps b/c) without constructing a Builder, and
// agent-3's future Run can reuse them as ground truth.

// entity_type attribute values for <ogrn_check>. The set is closed (emitted
// without escaping); "null" matches the agent-3 prompt contract
// entity_type="LEGAL_ENTITY|INDIVIDUAL_ENTREPRENEUR|null".
const (
	EntityLegal      = "LEGAL_ENTITY"
	EntityIndividual = "INDIVIDUAL_ENTREPRENEUR"
	EntityNull       = "null"
)

// FNS weight vectors for the ИНН control digits.
var (
	// innW10: 10-digit organisation INN, single control digit.
	innW10 = [9]int{2, 4, 10, 3, 5, 9, 4, 6, 8}
	// innW11/innW12: 12-digit (individual) INN, digits 11 and 12.
	innW11 = [10]int{7, 2, 4, 10, 3, 5, 9, 4, 6, 8}
	innW12 = [11]int{3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8}
)

// toDigits converts s to its decimal digits, rejecting (ok=false) any
// non-digit byte. Length/shape gating happens at the call site so checksum
// math never runs on garbage (code-architect NTH-3).
func toDigits(s string) (d []int, ok bool) {
	d = make([]int, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return nil, false
		}
		d[i] = int(c - '0')
	}
	return d, true
}

// innControl returns (Σ dᵢ·wᵢ) mod 11 mod 10 — the FNS rule where a
// remainder of 10 maps to control digit 0.
func innControl(d []int, w []int) int {
	sum := 0
	for i, wi := range w {
		sum += d[i] * wi
	}
	return sum % 11 % 10
}

// ValidateINN reports whether inn passes the FNS control-digit algorithm.
// 10-digit (organisation) and 12-digit (individual / sole entrepreneur)
// forms are accepted; any other length or a non-digit byte ⇒ false.
func ValidateINN(inn string) bool {
	d, ok := toDigits(inn)
	if !ok {
		return false
	}
	switch len(d) {
	case 10:
		return d[9] == innControl(d, innW10[:])
	case 12:
		return d[10] == innControl(d, innW11[:]) &&
			d[11] == innControl(d, innW12[:])
	default:
		return false
	}
}

// safeEntityType enforces the closed-set invariant at the *write site*:
// ValidationFacts emits entity_type WITHOUT attribute-escaping because the
// value is system-derived, not user-controlled. That safety must be enforced,
// not assumed — anything other than the two real registrant constants is
// collapsed to EntityNull so a future change to ValidateOGRN's contract can
// never silently turn entity_type into an unescaped attribute-injection sink
// (security-engineer LOW-6).
func safeEntityType(e string) string {
	switch e {
	case EntityLegal, EntityIndividual:
		return e
	default:
		return EntityNull
	}
}

// ValidateOGRN reports whether ogrn passes the FNS control-digit algorithm
// and classifies the registrant: 13-digit ⇒ LEGAL_ENTITY (ОГРН), 15-digit ⇒
// INDIVIDUAL_ENTREPRENEUR (ОГРНИП). On any failure entityType is EntityNull,
// matching the envelope's entity_type="…|null" contract.
func ValidateOGRN(ogrn string) (valid bool, entityType string) {
	d, ok := toDigits(ogrn)
	if !ok {
		return false, EntityNull
	}
	switch len(d) {
	case 13:
		if ogrnControl(d, 12, 11) == d[12] {
			return true, EntityLegal
		}
	case 15:
		if ogrnControl(d, 14, 13) == d[14] {
			return true, EntityIndividual
		}
	}
	return false, EntityNull
}

// ogrnControl builds the integer from the first n digits, takes it modulo
// mod, and returns the last decimal digit of the remainder (FNS rule:
// remainder % 10, so a remainder of 10 maps to 0). n ≤ 14 ⇒ the value is
// ≤ 1e14 ≪ math.MaxUint64 (1.8e19), so uint64 needs no big-int dependency.
func ogrnControl(d []int, n, mod int) int {
	var v uint64
	for i := 0; i < n; i++ {
		v = v*10 + uint64(d[i])
	}
	return int(v % uint64(mod) % 10)
}
