package model

import (
	"encoding/json"
	"testing"
)

func TestDocumentStructure_JSONRoundTrip(t *testing.T) {
	original := DocumentStructure{
		DocumentID: "doc-1",
		Sections: []Section{
			{
				Number: "1",
				Title:  "Предмет договора",
				Clauses: []Clause{
					{
						Number:  "1.1",
						Content: "Исполнитель обязуется оказать услуги.",
						SubClauses: []SubClause{
							{Number: "1.1.1", Content: "Услуги включают консультирование."},
							{Number: "1.1.2", Content: "Услуги включают разработку."},
						},
					},
					{
						Number:  "1.2",
						Content: "Заказчик обязуется оплатить услуги.",
					},
				},
			},
			{
				Number:  "2",
				Title:   "Сроки",
				Content: "Договор действует с 01.01.2026 по 31.12.2026.",
			},
		},
		Appendices: []Appendix{
			{
				Number:  "1",
				Title:   "Перечень услуг",
				Content: "Детальное описание оказываемых услуг.",
			},
		},
		PartyDetails: []PartyDetails{
			{
				Name:           "ООО \"Ромашка\"",
				INN:            "7701234567",
				OGRN:           "1027700132195",
				Address:        "г. Москва, ул. Ленина, д. 1",
				Representative: "Иванов Иван Иванович",
			},
			{
				Name:    "ИП Петров П.П.",
				INN:     "770987654321",
				Address: "г. Москва, ул. Пушкина, д. 2",
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored DocumentStructure
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.DocumentID != original.DocumentID {
		t.Errorf("DocumentID = %q, want %q", restored.DocumentID, original.DocumentID)
	}
	if len(restored.Sections) != len(original.Sections) {
		t.Fatalf("Sections count = %d, want %d", len(restored.Sections), len(original.Sections))
	}

	// Verify first section with nested clauses
	s0 := restored.Sections[0]
	if s0.Number != "1" {
		t.Errorf("Section[0].Number = %q, want %q", s0.Number, "1")
	}
	if s0.Title != "Предмет договора" {
		t.Errorf("Section[0].Title = %q, want %q", s0.Title, "Предмет договора")
	}
	if len(s0.Clauses) != 2 {
		t.Fatalf("Section[0].Clauses count = %d, want 2", len(s0.Clauses))
	}
	if s0.Clauses[0].Number != "1.1" {
		t.Errorf("Clause[0].Number = %q, want %q", s0.Clauses[0].Number, "1.1")
	}
	if len(s0.Clauses[0].SubClauses) != 2 {
		t.Fatalf("Clause[0].SubClauses count = %d, want 2", len(s0.Clauses[0].SubClauses))
	}
	if s0.Clauses[0].SubClauses[0].Number != "1.1.1" {
		t.Errorf("SubClause[0].Number = %q, want %q", s0.Clauses[0].SubClauses[0].Number, "1.1.1")
	}

	// Verify appendices
	if len(restored.Appendices) != 1 {
		t.Fatalf("Appendices count = %d, want 1", len(restored.Appendices))
	}
	if restored.Appendices[0].Title != "Перечень услуг" {
		t.Errorf("Appendix[0].Title = %q, want %q", restored.Appendices[0].Title, "Перечень услуг")
	}

	// Verify party details
	if len(restored.PartyDetails) != 2 {
		t.Fatalf("PartyDetails count = %d, want 2", len(restored.PartyDetails))
	}
	if restored.PartyDetails[0].INN != "7701234567" {
		t.Errorf("PartyDetails[0].INN = %q, want %q", restored.PartyDetails[0].INN, "7701234567")
	}
	if restored.PartyDetails[1].Representative != "" {
		t.Errorf("PartyDetails[1].Representative = %q, want empty", restored.PartyDetails[1].Representative)
	}
}

func TestDocumentStructure_MinimalJSON(t *testing.T) {
	original := DocumentStructure{
		DocumentID: "doc-2",
		Sections: []Section{
			{Number: "1", Title: "Единственный раздел"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Optional fields should be omitted
	omittedFields := []string{"appendices", "party_details"}
	for _, field := range omittedFields {
		if _, exists := raw[field]; exists {
			t.Errorf("field %q should be omitted when empty", field)
		}
	}

	// Required fields should be present
	requiredFields := []string{"document_id", "sections"}
	for _, field := range requiredFields {
		if _, exists := raw[field]; !exists {
			t.Errorf("field %q should be present", field)
		}
	}
}

func TestSection_JSONOmitsEmptyOptionalFields(t *testing.T) {
	section := Section{
		Number: "1",
		Title:  "Раздел без пунктов",
	}

	data, err := json.Marshal(section)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	omittedFields := []string{"content", "clauses"}
	for _, field := range omittedFields {
		if _, exists := raw[field]; exists {
			t.Errorf("field %q should be omitted when empty", field)
		}
	}
}

func TestClause_JSONOmitsEmptySubClauses(t *testing.T) {
	clause := Clause{
		Number:  "1.1",
		Content: "Простой пункт без подпунктов.",
	}

	data, err := json.Marshal(clause)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, exists := raw["sub_clauses"]; exists {
		t.Error("sub_clauses should be omitted when empty")
	}
}

func TestPartyDetails_JSONRoundTrip(t *testing.T) {
	original := PartyDetails{
		Name:           "ООО \"Тестовая компания\"",
		INN:            "7712345678",
		OGRN:           "1027700000001",
		Address:        "г. Санкт-Петербург, Невский пр., д. 10",
		Representative: "Сидоров Сидор Сидорович",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored PartyDetails
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.Name != original.Name {
		t.Errorf("Name = %q, want %q", restored.Name, original.Name)
	}
	if restored.INN != original.INN {
		t.Errorf("INN = %q, want %q", restored.INN, original.INN)
	}
	if restored.OGRN != original.OGRN {
		t.Errorf("OGRN = %q, want %q", restored.OGRN, original.OGRN)
	}
	if restored.Address != original.Address {
		t.Errorf("Address = %q, want %q", restored.Address, original.Address)
	}
	if restored.Representative != original.Representative {
		t.Errorf("Representative = %q, want %q", restored.Representative, original.Representative)
	}
}

func TestPartyDetails_JSONOmitsEmptyOptionalFields(t *testing.T) {
	party := PartyDetails{
		Name: "ООО \"Минимум\"",
	}

	data, err := json.Marshal(party)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	omittedFields := []string{"inn", "ogrn", "address", "representative"}
	for _, field := range omittedFields {
		if _, exists := raw[field]; exists {
			t.Errorf("field %q should be omitted when empty", field)
		}
	}

	if _, exists := raw["name"]; !exists {
		t.Error("name should always be present")
	}
}
