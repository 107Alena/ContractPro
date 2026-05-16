package promptbuilder

import "testing"

func TestValidateINN(t *testing.T) {
	cases := []struct {
		name string
		inn  string
		want bool
	}{
		{"acceptance step 3: valid 10-digit org INN", "7707083893", true},
		{"valid 12-digit individual INN", "500100732259", true},
		{"10-digit wrong control digit", "7707083894", false},
		{"12-digit wrong control digit", "500100732258", false},
		{"too short (9)", "770708389", false},
		{"11 digits (neither form)", "77070838930", false},
		{"non-digit byte", "770708389X", false},
		{"empty", "", false},
		{"all zeros 10-digit (checksum-valid edge)", "0000000000", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ValidateINN(c.inn); got != c.want {
				t.Fatalf("ValidateINN(%q) = %v, want %v", c.inn, got, c.want)
			}
		})
	}
}

// TestSafeEntityType pins the closed-set write-site guard
// (security-engineer LOW-6): only the two real registrant constants survive;
// anything else collapses to EntityNull so entity_type can never become an
// unescaped attribute-injection sink.
func TestSafeEntityType(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{EntityLegal, EntityLegal},
		{EntityIndividual, EntityIndividual},
		{EntityNull, EntityNull},
		{"", EntityNull},
		{`x" valid="true`, EntityNull},
		{"LEGAL_ENTITY_EXTRA", EntityNull},
		{"<inject>", EntityNull},
	}
	for _, c := range cases {
		if got := safeEntityType(c.in); got != c.want {
			t.Fatalf("safeEntityType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidateOGRN(t *testing.T) {
	cases := []struct {
		name       string
		ogrn       string
		wantValid  bool
		wantEntity string
	}{
		{"valid 13-digit OGRN (legal entity)", "1027700132195", true, EntityLegal},
		{"valid 15-digit OGRNIP (individual entrepreneur)", "304500116000157", true, EntityIndividual},
		{"acceptance step 4: invalid OGRN", "1027700132194", false, EntityNull},
		{"15-digit wrong control digit", "304500116000158", false, EntityNull},
		{"too short (12)", "102770013219", false, EntityNull},
		{"14 digits (neither form)", "10277001321950", false, EntityNull},
		{"non-digit byte", "10277001321X5", false, EntityNull},
		{"empty", "", false, EntityNull},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotValid, gotEntity := ValidateOGRN(c.ogrn)
			if gotValid != c.wantValid || gotEntity != c.wantEntity {
				t.Fatalf("ValidateOGRN(%q) = (%v,%q), want (%v,%q)",
					c.ogrn, gotValid, gotEntity, c.wantValid, c.wantEntity)
			}
		})
	}
}
