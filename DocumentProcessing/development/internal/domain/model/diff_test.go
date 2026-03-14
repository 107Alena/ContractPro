package model

import (
	"encoding/json"
	"testing"
)

func TestVersionDiffResult_JSONRoundTrip(t *testing.T) {
	diff := VersionDiffResult{
		DocumentID:      "doc-001",
		BaseVersionID:   "v1",
		TargetVersionID: "v2",
		TextDiffs: []TextDiffEntry{
			{
				Type:       DiffTypeAdded,
				Path:       "Раздел 3 / Пункт 3.1",
				NewContent: "Исполнитель несёт ответственность за качество услуг.",
			},
			{
				Type:       DiffTypeRemoved,
				Path:       "Раздел 2 / Пункт 2.3",
				OldContent: "Оплата производится в течение 5 рабочих дней.",
			},
			{
				Type:       DiffTypeModified,
				Path:       "Раздел 1 / Пункт 1.1",
				OldContent: "Срок действия — 12 месяцев.",
				NewContent: "Срок действия — 24 месяца.",
			},
		},
		StructuralDiffs: []StructuralDiffEntry{
			{
				Type:        DiffTypeAdded,
				NodeType:    NodeTypeClause,
				NodeID:      "clause-3-1",
				Path:        "Раздел 3 / Пункт 3.1",
				Description: "Добавлен пункт об ответственности",
			},
			{
				Type:     DiffTypeRemoved,
				NodeType: NodeTypeClause,
				NodeID:   "clause-2-3",
				Path:     "Раздел 2 / Пункт 2.3",
			},
		},
	}

	data, err := json.Marshal(diff)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got VersionDiffResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.DocumentID != diff.DocumentID {
		t.Errorf("DocumentID = %q, want %q", got.DocumentID, diff.DocumentID)
	}
	if got.BaseVersionID != diff.BaseVersionID {
		t.Errorf("BaseVersionID = %q, want %q", got.BaseVersionID, diff.BaseVersionID)
	}
	if got.TargetVersionID != diff.TargetVersionID {
		t.Errorf("TargetVersionID = %q, want %q", got.TargetVersionID, diff.TargetVersionID)
	}
	if len(got.TextDiffs) != 3 {
		t.Fatalf("TextDiffs count = %d, want 3", len(got.TextDiffs))
	}
	if len(got.StructuralDiffs) != 2 {
		t.Fatalf("StructuralDiffs count = %d, want 2", len(got.StructuralDiffs))
	}

	// Verify specific entries
	if got.TextDiffs[0].Type != DiffTypeAdded {
		t.Errorf("TextDiffs[0].Type = %q, want %q", got.TextDiffs[0].Type, DiffTypeAdded)
	}
	if got.TextDiffs[2].OldContent != "Срок действия — 12 месяцев." {
		t.Errorf("TextDiffs[2].OldContent = %q", got.TextDiffs[2].OldContent)
	}
	if got.StructuralDiffs[0].NodeType != NodeTypeClause {
		t.Errorf("StructuralDiffs[0].NodeType = %q, want %q", got.StructuralDiffs[0].NodeType, NodeTypeClause)
	}
}

func TestTextDiffEntry_JSONOmitempty(t *testing.T) {
	tests := []struct {
		name     string
		entry    TextDiffEntry
		absent   []string
		present  []string
	}{
		{
			name:    "added entry omits old_content",
			entry:   TextDiffEntry{Type: DiffTypeAdded, NewContent: "new text"},
			absent:  []string{"path", "old_content"},
			present: []string{"type", "new_content"},
		},
		{
			name:    "removed entry omits new_content",
			entry:   TextDiffEntry{Type: DiffTypeRemoved, OldContent: "old text"},
			absent:  []string{"path", "new_content"},
			present: []string{"type", "old_content"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.entry)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var raw map[string]interface{}
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("Unmarshal to map: %v", err)
			}

			for _, field := range tc.absent {
				if _, exists := raw[field]; exists {
					t.Errorf("field %q should be omitted", field)
				}
			}
			for _, field := range tc.present {
				if _, exists := raw[field]; !exists {
					t.Errorf("field %q should be present", field)
				}
			}
		})
	}
}

func TestStructuralDiffEntry_JSONOmitempty(t *testing.T) {
	entry := StructuralDiffEntry{
		Type:     DiffTypeAdded,
		NodeType: NodeTypeSection,
		NodeID:   "sec-3",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}

	for _, field := range []string{"path", "description"} {
		if _, exists := raw[field]; exists {
			t.Errorf("field %q should be omitted when empty", field)
		}
	}
	for _, field := range []string{"type", "node_type", "node_id"} {
		if _, exists := raw[field]; !exists {
			t.Errorf("field %q should be present", field)
		}
	}
}

func TestDiffTypeConstants(t *testing.T) {
	types := []struct {
		got  DiffType
		want string
	}{
		{DiffTypeAdded, "added"},
		{DiffTypeRemoved, "removed"},
		{DiffTypeModified, "modified"},
	}
	for _, tc := range types {
		if string(tc.got) != tc.want {
			t.Errorf("DiffType %q != %q", tc.got, tc.want)
		}
	}
}

func TestVersionDiffResult_EmptyDiffs(t *testing.T) {
	diff := VersionDiffResult{
		DocumentID:      "doc-001",
		BaseVersionID:   "v1",
		TargetVersionID: "v2",
		TextDiffs:       []TextDiffEntry{},
		StructuralDiffs: []StructuralDiffEntry{},
	}

	data, err := json.Marshal(diff)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got VersionDiffResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(got.TextDiffs) != 0 {
		t.Errorf("TextDiffs should be empty, got %d", len(got.TextDiffs))
	}
	if len(got.StructuralDiffs) != 0 {
		t.Errorf("StructuralDiffs should be empty, got %d", len(got.StructuralDiffs))
	}
}
