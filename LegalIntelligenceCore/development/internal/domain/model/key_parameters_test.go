package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPartyRoleType_IsValid(t *testing.T) {
	for _, r := range []PartyRoleType{PartyRoleCustomer, PartyRoleContractor, PartyRoleSeller,
		PartyRoleBuyer, PartyRoleLessor, PartyRoleLessee, PartyRoleLicensor, PartyRoleLicensee,
		PartyRoleParty} {
		if !r.IsValid() {
			t.Errorf("%q must be valid", r)
		}
	}
	if PartyRoleType("Customer").IsValid() {
		t.Error("Customer (mixed case) must NOT be valid")
	}
	if PartyRoleType("").IsValid() {
		t.Error("empty must NOT be valid")
	}
}

func TestKeyParameters_FullRoundTrip(t *testing.T) {
	price := "500 000 руб., оплата по факту"
	duration := "С 01.04.2026 до 31.12.2026"
	penalties := "0,1% от суммы за день просрочки"
	jurisdiction := "Арбитражный суд г. Москвы"
	law := "Российское право"
	term := "Одностороннее расторжение"
	accept := "По товарной накладной"

	in := KeyParameters{
		Parties:      []string{"ООО „Альфа\"", "ООО „Бета\""},
		Subject:      "Поставка офисной мебели",
		Price:        &price,
		Duration:     &duration,
		Penalties:    &penalties,
		Jurisdiction: &jurisdiction,
		InternalExtras: &KeyParametersInternalExtras{
			ApplicableLaw:       &law,
			Termination:         &term,
			AcceptanceProcedure: &accept,
			PartyRoles: []PartyRole{
				{
					Name: "ООО „Альфа\"",
					Role: PartyRoleSeller,
					INN:  strPtr("7707083893"),
					OGRN: strPtr("1027700132195"),
				},
			},
			KeyDates: []KeyDate{
				{Label: "Срок поставки", Value: "30.04.2026", ClauseRef: "sec-3.1"},
			},
		},
		PromptInjectionDetected: false,
	}

	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, must := range []string{`"parties":`, `"internal_extras":`, `"party_roles":`, `"role":"seller"`, `"inn":"7707083893"`} {
		if !strings.Contains(string(b), must) {
			t.Errorf("missing substring %q in JSON:\n%s", must, b)
		}
	}

	var got KeyParameters
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, in)
	}
}

func TestKeyParameters_NullsPreserved(t *testing.T) {
	// price/duration/penalties/jurisdiction are wire-required with null option
	// per ai-agents-pipeline.md §2 schema; the JSON output MUST include the
	// fields with null (NOT omit them).
	in := KeyParameters{
		Parties:                 []string{"ИП Сидоров"},
		Subject:                 "услуги",
		Price:                   nil,
		Duration:                nil,
		Penalties:               nil,
		Jurisdiction:            nil,
		PromptInjectionDetected: true,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, must := range []string{`"price":null`, `"duration":null`, `"penalties":null`, `"jurisdiction":null`} {
		if !strings.Contains(string(b), must) {
			t.Errorf("expected %q in JSON (nullable, not omitempty), got %s", must, b)
		}
	}
	if strings.Contains(string(b), `"internal_extras"`) {
		t.Errorf("internal_extras must be omitted when nil, got %s", b)
	}
}

func TestPartyRole_NullableFieldsSerialiseAsNull(t *testing.T) {
	// ai-agents-pipeline.md §2 schema declares inn/ogrn/address/signatory/
	// signatory_authority/clause_ref as `type: ["string","null"]` — they must
	// serialise as null when unset, not be omitted.
	r := PartyRole{Name: "ИП Сидоров", Role: PartyRoleParty}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, must := range []string{`"inn":null`, `"ogrn":null`, `"address":null`,
		`"signatory":null`, `"signatory_authority":null`, `"clause_ref":null`} {
		if !strings.Contains(string(b), must) {
			t.Errorf("expected %q in JSON, got %s", must, b)
		}
	}
}

func strPtr(s string) *string { return &s }
