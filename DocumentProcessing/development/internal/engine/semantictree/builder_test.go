package semantictree

import (
	"context"
	"encoding/json"
	"testing"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

func TestBuilder_InterfaceCompliance(t *testing.T) {
	var _ port.SemanticTreeBuilderPort = (*Builder)(nil)
}

func TestBuilder_FullContract(t *testing.T) {
	b := NewBuilder()
	structure := &model.DocumentStructure{
		DocumentID: "doc-1",
		Sections: []model.Section{
			{
				Number: "1",
				Title:  "Предмет договора",
				Clauses: []model.Clause{
					{
						Number:  "1.1",
						Content: "Исполнитель обязуется оказать услуги.",
						SubClauses: []model.SubClause{
							{Number: "1.1.1", Content: "Перечень услуг указан в Приложении 1."},
						},
					},
					{Number: "1.2", Content: "Заказчик обязуется оплатить услуги."},
				},
			},
			{
				Number:  "2",
				Title:   "Стоимость и порядок расчётов",
				Clauses: []model.Clause{
					{Number: "2.1", Content: "Стоимость услуг составляет 100 000 руб."},
				},
			},
		},
		Appendices: []model.Appendix{
			{Number: "1", Title: "Перечень услуг", Content: "Консультационные услуги по вопросам ИТ."},
		},
		PartyDetails: []model.PartyDetails{
			{
				Name:           "ООО «Ромашка»",
				INN:            "7701234567",
				OGRN:           "1027700132195",
				Address:        "г. Москва, ул. Ленина, д. 1",
				Representative: "Иванов И.И.",
			},
			{
				Name: "ИП Петров П.П.",
				INN:  "770987654321",
			},
		},
	}

	tree, err := b.Build(context.Background(), nil, structure)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tree.DocumentID != "doc-1" {
		t.Errorf("DocumentID = %q, want %q", tree.DocumentID, "doc-1")
	}
	if tree.Root == nil {
		t.Fatal("Root is nil")
	}
	if tree.Root.Type != model.NodeTypeRoot {
		t.Errorf("Root.Type = %q, want %q", tree.Root.Type, model.NodeTypeRoot)
	}

	// Root children: 2 sections + 1 appendix + 2 parties = 5
	if got := len(tree.Root.Children); got != 5 {
		t.Fatalf("Root.Children count = %d, want 5", got)
	}

	// Section 1
	sec1 := tree.Root.Children[0]
	if sec1.Type != model.NodeTypeSection {
		t.Errorf("section1.Type = %q, want SECTION", sec1.Type)
	}
	if sec1.ID != "section-1" {
		t.Errorf("section1.ID = %q, want %q", sec1.ID, "section-1")
	}
	if sec1.Metadata["number"] != "1" || sec1.Metadata["title"] != "Предмет договора" {
		t.Errorf("section1 metadata = %v", sec1.Metadata)
	}
	// Section 1 has 2 clauses (no content)
	if got := len(sec1.Children); got != 2 {
		t.Fatalf("section1.Children = %d, want 2", got)
	}

	// Clause 1.1 with sub-clause
	cl11 := sec1.Children[0]
	if cl11.Type != model.NodeTypeClause {
		t.Errorf("clause1.1.Type = %q, want CLAUSE", cl11.Type)
	}
	if cl11.Metadata["number"] != "1.1" {
		t.Errorf("clause1.1 number = %q", cl11.Metadata["number"])
	}
	if cl11.Content != "Исполнитель обязуется оказать услуги." {
		t.Errorf("clause1.1 content = %q", cl11.Content)
	}
	if got := len(cl11.Children); got != 1 {
		t.Fatalf("clause1.1 children = %d, want 1 (sub-clause)", got)
	}

	// Sub-clause 1.1.1
	sub111 := cl11.Children[0]
	if sub111.Type != model.NodeTypeClause {
		t.Errorf("subclause1.1.1.Type = %q, want CLAUSE", sub111.Type)
	}
	if sub111.ID != "subclause-1.1.1" {
		t.Errorf("subclause1.1.1.ID = %q", sub111.ID)
	}
	if sub111.Metadata["number"] != "1.1.1" {
		t.Errorf("subclause1.1.1 number = %q", sub111.Metadata["number"])
	}

	// Appendix
	app := tree.Root.Children[2]
	if app.Type != model.NodeTypeAppendix {
		t.Errorf("appendix.Type = %q, want APPENDIX", app.Type)
	}
	if app.Metadata["number"] != "1" || app.Metadata["title"] != "Перечень услуг" {
		t.Errorf("appendix metadata = %v", app.Metadata)
	}
	if got := len(app.Children); got != 1 {
		t.Errorf("appendix children = %d, want 1 (text node)", got)
	}

	// Party 1 (full details)
	p1 := tree.Root.Children[3]
	if p1.Type != model.NodeTypePartyDetails {
		t.Errorf("party1.Type = %q, want PARTY_DETAILS", p1.Type)
	}
	if p1.Content != "ООО «Ромашка»" {
		t.Errorf("party1.Content = %q", p1.Content)
	}
	if p1.Metadata["inn"] != "7701234567" {
		t.Errorf("party1 inn = %q", p1.Metadata["inn"])
	}
	if p1.Metadata["representative"] != "Иванов И.И." {
		t.Errorf("party1 representative = %q", p1.Metadata["representative"])
	}

	// Party 2 (partial details — no ogrn, address, representative)
	p2 := tree.Root.Children[4]
	if _, ok := p2.Metadata["ogrn"]; ok {
		t.Error("party2 should not have ogrn in metadata")
	}
	if _, ok := p2.Metadata["address"]; ok {
		t.Error("party2 should not have address in metadata")
	}
}

func TestBuilder_SectionsOnly(t *testing.T) {
	b := NewBuilder()
	structure := &model.DocumentStructure{
		DocumentID: "doc-2",
		Sections: []model.Section{
			{Number: "1", Title: "Общие положения"},
			{Number: "2", Title: "Права и обязанности"},
		},
	}

	tree, err := b.Build(context.Background(), nil, structure)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(tree.Root.Children); got != 2 {
		t.Errorf("Root.Children = %d, want 2", got)
	}
	for i, child := range tree.Root.Children {
		if child.Type != model.NodeTypeSection {
			t.Errorf("child[%d].Type = %q, want SECTION", i, child.Type)
		}
		if len(child.Children) != 0 {
			t.Errorf("child[%d].Children = %d, want 0 (no content, no clauses)", i, len(child.Children))
		}
	}
}

func TestBuilder_EmptyStructure(t *testing.T) {
	b := NewBuilder()
	structure := &model.DocumentStructure{DocumentID: "doc-empty"}

	tree, err := b.Build(context.Background(), nil, structure)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree.Root.Type != model.NodeTypeRoot {
		t.Errorf("Root.Type = %q, want ROOT", tree.Root.Type)
	}
	if len(tree.Root.Children) != 0 {
		t.Errorf("Root.Children = %d, want 0", len(tree.Root.Children))
	}
}

func TestBuilder_AppendicesOnly(t *testing.T) {
	b := NewBuilder()
	structure := &model.DocumentStructure{
		DocumentID: "doc-app",
		Appendices: []model.Appendix{
			{Number: "1", Title: "Спецификация", Content: "Детали спецификации."},
			{Number: "2", Title: "Акт приёма", Content: ""},
		},
	}

	tree, err := b.Build(context.Background(), nil, structure)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(tree.Root.Children); got != 2 {
		t.Errorf("Root.Children = %d, want 2", got)
	}

	// Appendix 1 has content → 1 TextNode child
	if got := len(tree.Root.Children[0].Children); got != 1 {
		t.Errorf("appendix1 children = %d, want 1", got)
	}
	// Appendix 2 has no content → 0 children
	if got := len(tree.Root.Children[1].Children); got != 0 {
		t.Errorf("appendix2 children = %d, want 0", got)
	}
}

func TestBuilder_PartyDetailsOnly(t *testing.T) {
	b := NewBuilder()
	structure := &model.DocumentStructure{
		DocumentID: "doc-pd",
		PartyDetails: []model.PartyDetails{
			{Name: "ООО «Альфа»", INN: "1234567890", Address: "г. Москва"},
		},
	}

	tree, err := b.Build(context.Background(), nil, structure)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(tree.Root.Children); got != 1 {
		t.Errorf("Root.Children = %d, want 1", got)
	}
	p := tree.Root.Children[0]
	if p.Type != model.NodeTypePartyDetails {
		t.Errorf("Type = %q, want PARTY_DETAILS", p.Type)
	}
	if p.Metadata["name"] != "ООО «Альфа»" {
		t.Errorf("name = %q", p.Metadata["name"])
	}
	if p.Metadata["inn"] != "1234567890" {
		t.Errorf("inn = %q", p.Metadata["inn"])
	}
	if p.Metadata["address"] != "г. Москва" {
		t.Errorf("address = %q", p.Metadata["address"])
	}
	// ogrn and representative should be absent
	if _, ok := p.Metadata["ogrn"]; ok {
		t.Error("ogrn should not be in metadata")
	}
	if _, ok := p.Metadata["representative"]; ok {
		t.Error("representative should not be in metadata")
	}
}

func TestBuilder_SectionWithContent(t *testing.T) {
	b := NewBuilder()
	structure := &model.DocumentStructure{
		DocumentID: "doc-sc",
		Sections: []model.Section{
			{
				Number:  "1",
				Title:   "Введение",
				Content: "Настоящий договор регулирует...",
			},
		},
	}

	tree, err := b.Build(context.Background(), nil, structure)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sec := tree.Root.Children[0]
	if got := len(sec.Children); got != 1 {
		t.Fatalf("section children = %d, want 1 (text node)", got)
	}
	textNode := sec.Children[0]
	if textNode.Type != model.NodeTypeText {
		t.Errorf("textNode.Type = %q, want TEXT", textNode.Type)
	}
	if textNode.Content != "Настоящий договор регулирует..." {
		t.Errorf("textNode.Content = %q", textNode.Content)
	}
}

func TestBuilder_SectionWithContentAndClauses(t *testing.T) {
	b := NewBuilder()
	structure := &model.DocumentStructure{
		DocumentID: "doc-scc",
		Sections: []model.Section{
			{
				Number:  "1",
				Title:   "Предмет",
				Content: "Вводный текст раздела.",
				Clauses: []model.Clause{
					{Number: "1.1", Content: "Пункт 1.1."},
				},
			},
		},
	}

	tree, err := b.Build(context.Background(), nil, structure)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sec := tree.Root.Children[0]
	// TextNode first, then ClauseNode
	if got := len(sec.Children); got != 2 {
		t.Fatalf("section children = %d, want 2", got)
	}
	if sec.Children[0].Type != model.NodeTypeText {
		t.Errorf("first child type = %q, want TEXT", sec.Children[0].Type)
	}
	if sec.Children[1].Type != model.NodeTypeClause {
		t.Errorf("second child type = %q, want CLAUSE", sec.Children[1].Type)
	}
}

func TestBuilder_ClauseWithSubClauses(t *testing.T) {
	b := NewBuilder()
	structure := &model.DocumentStructure{
		DocumentID: "doc-sub",
		Sections: []model.Section{
			{
				Number: "1",
				Title:  "Раздел",
				Clauses: []model.Clause{
					{
						Number:  "1.1",
						Content: "Основной пункт.",
						SubClauses: []model.SubClause{
							{Number: "1.1.1", Content: "Подпункт А."},
							{Number: "1.1.2", Content: "Подпункт Б."},
						},
					},
				},
			},
		},
	}

	tree, err := b.Build(context.Background(), nil, structure)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clause := tree.Root.Children[0].Children[0]
	if got := len(clause.Children); got != 2 {
		t.Fatalf("clause children = %d, want 2 sub-clauses", got)
	}
	for _, sub := range clause.Children {
		if sub.Type != model.NodeTypeClause {
			t.Errorf("sub-clause type = %q, want CLAUSE", sub.Type)
		}
	}
	if clause.Children[0].Metadata["number"] != "1.1.1" {
		t.Errorf("sub1 number = %q", clause.Children[0].Metadata["number"])
	}
	if clause.Children[1].Metadata["number"] != "1.1.2" {
		t.Errorf("sub2 number = %q", clause.Children[1].Metadata["number"])
	}
}

func TestBuilder_JSONRoundTrip(t *testing.T) {
	b := NewBuilder()
	structure := &model.DocumentStructure{
		DocumentID: "doc-json",
		Sections: []model.Section{
			{
				Number: "1",
				Title:  "Раздел",
				Clauses: []model.Clause{
					{Number: "1.1", Content: "Пункт."},
				},
			},
		},
		Appendices: []model.Appendix{
			{Number: "1", Title: "Приложение", Content: "Текст приложения."},
		},
		PartyDetails: []model.PartyDetails{
			{Name: "ООО «Тест»", INN: "1234567890"},
		},
	}

	tree, err := b.Build(context.Background(), nil, structure)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored model.SemanticTree
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.DocumentID != tree.DocumentID {
		t.Errorf("DocumentID = %q, want %q", restored.DocumentID, tree.DocumentID)
	}
	if restored.Root == nil {
		t.Fatal("restored Root is nil")
	}
	if restored.Root.Type != model.NodeTypeRoot {
		t.Errorf("Root.Type = %q, want ROOT", restored.Root.Type)
	}
	if got := len(restored.Root.Children); got != 3 {
		t.Errorf("Root.Children = %d, want 3", got)
	}
}

func TestBuilder_WalkTraversal(t *testing.T) {
	b := NewBuilder()
	structure := &model.DocumentStructure{
		DocumentID: "doc-walk",
		Sections: []model.Section{
			{
				Number:  "1",
				Title:   "Раздел",
				Content: "Вводный текст.",
				Clauses: []model.Clause{
					{Number: "1.1", Content: "Пункт."},
				},
			},
		},
		PartyDetails: []model.PartyDetails{
			{Name: "Тест"},
		},
	}

	tree, err := b.Build(context.Background(), nil, structure)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var ids []string
	tree.Walk(func(n *model.SemanticNode) bool {
		ids = append(ids, n.ID)
		return true
	})

	// Expected DFS pre-order: root, section-1, text-1, clause-1.1, party-1
	expected := []string{"root", "section-1", "text-1", "clause-1.1", "party-1"}
	if len(ids) != len(expected) {
		t.Fatalf("walk visited %d nodes, want %d: %v", len(ids), len(expected), ids)
	}
	for i, want := range expected {
		if ids[i] != want {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want)
		}
	}
}

func TestBuilder_ContextCancellation(t *testing.T) {
	b := NewBuilder()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	structure := &model.DocumentStructure{DocumentID: "doc-cancel"}
	tree, err := b.Build(ctx, nil, structure)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if tree != nil {
		t.Error("tree should be nil on error")
	}

	if port.ErrorCode(err) != port.ErrCodeExtractionFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeExtractionFailed)
	}
}

func TestBuilder_NilStructure(t *testing.T) {
	b := NewBuilder()
	tree, err := b.Build(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error for nil structure")
	}
	if tree != nil {
		t.Error("tree should be nil on error")
	}
	if port.ErrorCode(err) != port.ErrCodeExtractionFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeExtractionFailed)
	}
}

func TestBuilder_NilText(t *testing.T) {
	b := NewBuilder()
	structure := &model.DocumentStructure{
		DocumentID: "doc-niltext",
		Sections: []model.Section{
			{Number: "1", Title: "Раздел"},
		},
	}

	tree, err := b.Build(context.Background(), nil, structure)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree.DocumentID != "doc-niltext" {
		t.Errorf("DocumentID = %q", tree.DocumentID)
	}
	if got := len(tree.Root.Children); got != 1 {
		t.Errorf("Root.Children = %d, want 1", got)
	}
}

func TestBuilder_MultipleAppendices(t *testing.T) {
	b := NewBuilder()
	structure := &model.DocumentStructure{
		DocumentID: "doc-multi-app",
		Appendices: []model.Appendix{
			{Number: "1", Title: "Первое", Content: "Содержание 1."},
			{Number: "2", Title: "Второе", Content: "Содержание 2."},
			{Number: "3", Title: "Третье", Content: "Содержание 3."},
		},
	}

	tree, err := b.Build(context.Background(), nil, structure)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(tree.Root.Children); got != 3 {
		t.Errorf("Root.Children = %d, want 3", got)
	}

	// Verify unique IDs
	ids := make(map[string]bool)
	tree.Walk(func(n *model.SemanticNode) bool {
		if ids[n.ID] {
			t.Errorf("duplicate ID: %q", n.ID)
		}
		ids[n.ID] = true
		return true
	})
}

func TestBuilder_UniqueNodeIDs(t *testing.T) {
	b := NewBuilder()
	structure := &model.DocumentStructure{
		DocumentID: "doc-unique",
		Sections: []model.Section{
			{
				Number:  "1",
				Title:   "Раздел 1",
				Content: "Текст 1.",
				Clauses: []model.Clause{
					{
						Number:  "1.1",
						Content: "Пункт 1.1.",
						SubClauses: []model.SubClause{
							{Number: "1.1.1", Content: "Подпункт."},
						},
					},
				},
			},
			{
				Number:  "2",
				Title:   "Раздел 2",
				Content: "Текст 2.",
			},
		},
		Appendices: []model.Appendix{
			{Number: "1", Title: "Приложение", Content: "Текст приложения."},
		},
		PartyDetails: []model.PartyDetails{
			{Name: "Сторона 1"},
			{Name: "Сторона 2"},
		},
	}

	tree, err := b.Build(context.Background(), nil, structure)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ids := make(map[string]int)
	tree.Walk(func(n *model.SemanticNode) bool {
		ids[n.ID]++
		return true
	})

	for id, count := range ids {
		if count > 1 {
			t.Errorf("ID %q appears %d times", id, count)
		}
	}

	// Verify we have the expected total number of nodes:
	// root + 2 sections + 2 text(section content) + 1 clause + 1 subclause + 1 appendix + 1 text(appendix content) + 2 parties = 11
	if got := len(ids); got != 11 {
		t.Errorf("total nodes = %d, want 11", got)
	}
}

func TestBuilder_DocumentIDFromStructure(t *testing.T) {
	b := NewBuilder()
	text := &model.ExtractedText{DocumentID: "text-id"}
	structure := &model.DocumentStructure{DocumentID: "structure-id"}

	tree, err := b.Build(context.Background(), text, structure)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// DocumentID should come from structure, not text
	if tree.DocumentID != "structure-id" {
		t.Errorf("DocumentID = %q, want %q", tree.DocumentID, "structure-id")
	}
}
