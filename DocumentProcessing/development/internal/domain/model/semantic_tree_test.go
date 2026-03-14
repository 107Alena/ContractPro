package model

import (
	"encoding/json"
	"testing"
)

func TestSemanticTree_JSONRoundTrip(t *testing.T) {
	tree := SemanticTree{
		DocumentID: "doc-001",
		Root: &SemanticNode{
			ID:   "root-1",
			Type: NodeTypeRoot,
			Children: []*SemanticNode{
				{
					ID:      "sec-1",
					Type:    NodeTypeSection,
					Content: "Раздел 1. Предмет договора",
					Metadata: map[string]string{
						"number": "1",
						"title":  "Предмет договора",
					},
					Children: []*SemanticNode{
						{
							ID:      "clause-1-1",
							Type:    NodeTypeClause,
							Content: "1.1. Исполнитель обязуется оказать Заказчику услуги.",
							Metadata: map[string]string{
								"number": "1.1",
							},
						},
						{
							ID:      "text-1",
							Type:    NodeTypeText,
							Content: "Дополнительный текст раздела.",
						},
					},
				},
				{
					ID:      "appendix-1",
					Type:    NodeTypeAppendix,
					Content: "Приложение 1. Перечень услуг",
					Metadata: map[string]string{
						"number": "1",
						"title":  "Перечень услуг",
					},
				},
				{
					ID:      "party-1",
					Type:    NodeTypePartyDetails,
					Content: "ООО «Ромашка», ИНН 7701234567",
				},
			},
		},
	}

	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got SemanticTree
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.DocumentID != tree.DocumentID {
		t.Errorf("DocumentID = %q, want %q", got.DocumentID, tree.DocumentID)
	}
	if got.Root == nil {
		t.Fatal("Root is nil after round-trip")
	}
	if got.Root.ID != "root-1" {
		t.Errorf("Root.ID = %q, want %q", got.Root.ID, "root-1")
	}
	if len(got.Root.Children) != 3 {
		t.Fatalf("Root.Children count = %d, want 3", len(got.Root.Children))
	}
	sec := got.Root.Children[0]
	if sec.Type != NodeTypeSection {
		t.Errorf("first child Type = %q, want %q", sec.Type, NodeTypeSection)
	}
	if sec.Metadata["number"] != "1" {
		t.Errorf("section metadata[number] = %q, want %q", sec.Metadata["number"], "1")
	}
	if len(sec.Children) != 2 {
		t.Fatalf("section children count = %d, want 2", len(sec.Children))
	}
	if sec.Children[0].Type != NodeTypeClause {
		t.Errorf("clause Type = %q, want %q", sec.Children[0].Type, NodeTypeClause)
	}
}

func TestSemanticTree_JSONOmitempty(t *testing.T) {
	node := SemanticNode{
		ID:   "n-1",
		Type: NodeTypeText,
	}
	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}

	for _, field := range []string{"content", "metadata", "children"} {
		if _, exists := raw[field]; exists {
			t.Errorf("field %q should be omitted when empty", field)
		}
	}
}

func TestSemanticTree_WalkCountsAllNodes(t *testing.T) {
	tree := SemanticTree{
		DocumentID: "doc-001",
		Root: &SemanticNode{
			ID:   "root",
			Type: NodeTypeRoot,
			Children: []*SemanticNode{
				{
					ID:   "sec-1",
					Type: NodeTypeSection,
					Children: []*SemanticNode{
						{ID: "clause-1", Type: NodeTypeClause},
						{ID: "clause-2", Type: NodeTypeClause},
					},
				},
				{
					ID:   "sec-2",
					Type: NodeTypeSection,
					Children: []*SemanticNode{
						{ID: "text-1", Type: NodeTypeText},
					},
				},
			},
		},
	}

	count := 0
	tree.Walk(func(n *SemanticNode) bool {
		count++
		return true
	})

	if count != 6 {
		t.Errorf("Walk visited %d nodes, want 6", count)
	}
}

func TestSemanticTree_WalkDepthFirstOrder(t *testing.T) {
	tree := SemanticTree{
		DocumentID: "doc-001",
		Root: &SemanticNode{
			ID:   "root",
			Type: NodeTypeRoot,
			Children: []*SemanticNode{
				{
					ID:   "A",
					Type: NodeTypeSection,
					Children: []*SemanticNode{
						{ID: "A1", Type: NodeTypeClause},
						{ID: "A2", Type: NodeTypeClause},
					},
				},
				{
					ID:   "B",
					Type: NodeTypeSection,
				},
			},
		},
	}

	var order []string
	tree.Walk(func(n *SemanticNode) bool {
		order = append(order, n.ID)
		return true
	})

	expected := []string{"root", "A", "A1", "A2", "B"}
	if len(order) != len(expected) {
		t.Fatalf("visited %d nodes, want %d", len(order), len(expected))
	}
	for i, id := range expected {
		if order[i] != id {
			t.Errorf("order[%d] = %q, want %q", i, order[i], id)
		}
	}
}

func TestSemanticTree_WalkEarlyStop(t *testing.T) {
	tree := SemanticTree{
		DocumentID: "doc-001",
		Root: &SemanticNode{
			ID:   "root",
			Type: NodeTypeRoot,
			Children: []*SemanticNode{
				{ID: "A", Type: NodeTypeSection},
				{ID: "B", Type: NodeTypeSection},
				{ID: "C", Type: NodeTypeSection},
			},
		},
	}

	count := 0
	completed := tree.Walk(func(n *SemanticNode) bool {
		count++
		return n.ID != "B" // stop at B
	})

	if completed {
		t.Error("Walk should return false when stopped early")
	}
	if count != 3 { // root, A, B
		t.Errorf("Walk visited %d nodes, want 3 (root, A, B)", count)
	}
}

func TestSemanticTree_WalkNilRoot(t *testing.T) {
	tree := SemanticTree{DocumentID: "doc-empty"}

	count := 0
	completed := tree.Walk(func(n *SemanticNode) bool {
		count++
		return true
	})

	if !completed {
		t.Error("Walk with nil root should return true")
	}
	if count != 0 {
		t.Errorf("Walk with nil root visited %d nodes, want 0", count)
	}
}

func TestNodeTypeConstants(t *testing.T) {
	types := []struct {
		got  NodeType
		want string
	}{
		{NodeTypeRoot, "ROOT"},
		{NodeTypeSection, "SECTION"},
		{NodeTypeClause, "CLAUSE"},
		{NodeTypeText, "TEXT"},
		{NodeTypeAppendix, "APPENDIX"},
		{NodeTypePartyDetails, "PARTY_DETAILS"},
	}
	for _, tc := range types {
		if string(tc.got) != tc.want {
			t.Errorf("NodeType %q != %q", tc.got, tc.want)
		}
	}
}
