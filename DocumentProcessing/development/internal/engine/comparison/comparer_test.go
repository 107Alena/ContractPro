package comparison

import (
	"context"
	"testing"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// --- Test helpers ---

// makeTree creates a SemanticTree with the given documentID and root node.
func makeTree(t *testing.T, docID string, root *model.SemanticNode) *model.SemanticTree {
	t.Helper()
	return &model.SemanticTree{
		DocumentID: docID,
		Root:       root,
	}
}

// makeRoot creates a ROOT node with the given children.
func makeRoot(t *testing.T, children ...*model.SemanticNode) *model.SemanticNode {
	t.Helper()
	return &model.SemanticNode{
		ID:       "root",
		Type:     model.NodeTypeRoot,
		Children: children,
	}
}

// makeSection creates a SECTION node.
func makeSection(t *testing.T, id, number, title string, children ...*model.SemanticNode) *model.SemanticNode {
	t.Helper()
	return &model.SemanticNode{
		ID:   id,
		Type: model.NodeTypeSection,
		Metadata: map[string]string{
			"number": number,
			"title":  title,
		},
		Children: children,
	}
}

// makeClause creates a CLAUSE node.
func makeClause(t *testing.T, id, number, content string, children ...*model.SemanticNode) *model.SemanticNode {
	t.Helper()
	return &model.SemanticNode{
		ID:      id,
		Type:    model.NodeTypeClause,
		Content: content,
		Metadata: map[string]string{
			"number": number,
		},
		Children: children,
	}
}

// makeAppendix creates an APPENDIX node.
func makeAppendix(t *testing.T, id, number, title string, children ...*model.SemanticNode) *model.SemanticNode {
	t.Helper()
	return &model.SemanticNode{
		ID:   id,
		Type: model.NodeTypeAppendix,
		Metadata: map[string]string{
			"number": number,
			"title":  title,
		},
		Children: children,
	}
}

// makeTextNode creates a TEXT node.
func makeTextNode(t *testing.T, id, content string) *model.SemanticNode {
	t.Helper()
	return &model.SemanticNode{
		ID:      id,
		Type:    model.NodeTypeText,
		Content: content,
	}
}

// makePartyDetails creates a PARTY_DETAILS node.
func makePartyDetails(t *testing.T, id, name string, extra map[string]string) *model.SemanticNode {
	t.Helper()
	meta := map[string]string{"name": name}
	for k, v := range extra {
		meta[k] = v
	}
	return &model.SemanticNode{
		ID:       id,
		Type:     model.NodeTypePartyDetails,
		Content:  name,
		Metadata: meta,
	}
}

// assertNoError fails the test if err is non-nil.
func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// assertExtractionError checks that err is a DomainError with EXTRACTION_FAILED code.
func assertExtractionError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !port.IsDomainError(err) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}
	if port.ErrorCode(err) != port.ErrCodeExtractionFailed {
		t.Fatalf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeExtractionFailed)
	}
}

// findStructDiff finds a structural diff entry by NodeID. Returns nil if not found.
func findStructDiff(diffs []model.StructuralDiffEntry, nodeID string) *model.StructuralDiffEntry {
	for i := range diffs {
		if diffs[i].NodeID == nodeID {
			return &diffs[i]
		}
	}
	return nil
}

// findTextDiffByPath finds a text diff entry by Path and Type. Returns nil if not found.
func findTextDiffByPath(diffs []model.TextDiffEntry, path string, dt model.DiffType) *model.TextDiffEntry {
	for i := range diffs {
		if diffs[i].Path == path && diffs[i].Type == dt {
			return &diffs[i]
		}
	}
	return nil
}

// --- Tests ---

func TestComparer_InterfaceCompliance(t *testing.T) {
	var _ port.VersionComparisonPort = (*Comparer)(nil)
}

func TestComparer_IdenticalTrees(t *testing.T) {
	c := NewComparer()

	section := makeSection(t, "section-1", "1", "Предмет договора",
		makeClause(t, "clause-1.1", "1.1", "Исполнитель обязуется оказать услуги."),
	)
	baseTree := makeTree(t, "doc-1", makeRoot(t, section))

	sectionTarget := makeSection(t, "section-1", "1", "Предмет договора",
		makeClause(t, "clause-1.1", "1.1", "Исполнитель обязуется оказать услуги."),
	)
	targetTree := makeTree(t, "doc-2", makeRoot(t, sectionTarget))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	if len(result.TextDiffs) != 0 {
		t.Errorf("TextDiffs = %d, want 0", len(result.TextDiffs))
	}
	if len(result.StructuralDiffs) != 0 {
		t.Errorf("StructuralDiffs = %d, want 0", len(result.StructuralDiffs))
	}
}

func TestComparer_AddedSection(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет"),
	))

	targetTree := makeTree(t, "doc-2", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет"),
		makeSection(t, "section-2", "2", "Стоимость"),
	))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	// section-2 should be added structurally
	if len(result.StructuralDiffs) != 1 {
		t.Fatalf("StructuralDiffs = %d, want 1", len(result.StructuralDiffs))
	}
	diff := result.StructuralDiffs[0]
	if diff.Type != model.DiffTypeAdded {
		t.Errorf("Type = %q, want %q", diff.Type, model.DiffTypeAdded)
	}
	if diff.NodeID != "section-2" {
		t.Errorf("NodeID = %q, want %q", diff.NodeID, "section-2")
	}
	if diff.NodeType != model.NodeTypeSection {
		t.Errorf("NodeType = %q, want %q", diff.NodeType, model.NodeTypeSection)
	}
}

func TestComparer_RemovedSection(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет"),
		makeSection(t, "section-2", "2", "Стоимость"),
	))

	targetTree := makeTree(t, "doc-2", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет"),
	))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	if len(result.StructuralDiffs) != 1 {
		t.Fatalf("StructuralDiffs = %d, want 1", len(result.StructuralDiffs))
	}
	diff := result.StructuralDiffs[0]
	if diff.Type != model.DiffTypeRemoved {
		t.Errorf("Type = %q, want %q", diff.Type, model.DiffTypeRemoved)
	}
	if diff.NodeID != "section-2" {
		t.Errorf("NodeID = %q, want %q", diff.NodeID, "section-2")
	}
}

func TestComparer_ModifiedClauseContent(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Стоимость 100 000 руб."),
		),
	))

	targetTree := makeTree(t, "doc-2", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Стоимость 200 000 руб."),
		),
	))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	// No structural changes — same structure.
	if len(result.StructuralDiffs) != 0 {
		t.Errorf("StructuralDiffs = %d, want 0", len(result.StructuralDiffs))
	}

	// One text modification.
	if len(result.TextDiffs) != 1 {
		t.Fatalf("TextDiffs = %d, want 1", len(result.TextDiffs))
	}
	td := result.TextDiffs[0]
	if td.Type != model.DiffTypeModified {
		t.Errorf("Type = %q, want %q", td.Type, model.DiffTypeModified)
	}
	if td.OldContent != "Стоимость 100 000 руб." {
		t.Errorf("OldContent = %q", td.OldContent)
	}
	if td.NewContent != "Стоимость 200 000 руб." {
		t.Errorf("NewContent = %q", td.NewContent)
	}
}

func TestComparer_AddedClause(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Пункт один."),
		),
	))

	targetTree := makeTree(t, "doc-2", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Пункт один."),
			makeClause(t, "clause-1.2", "1.2", "Пункт два."),
		),
	))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	// clause-1.2 added structurally.
	addedStruct := findStructDiff(result.StructuralDiffs, "clause-1.2")
	if addedStruct == nil {
		t.Fatal("expected structural diff for clause-1.2")
	}
	if addedStruct.Type != model.DiffTypeAdded {
		t.Errorf("Type = %q, want %q", addedStruct.Type, model.DiffTypeAdded)
	}

	// clause-1.2 added as text.
	addedText := findTextDiffByPath(result.TextDiffs, "Раздел 1 / Пункт 1.2", model.DiffTypeAdded)
	if addedText == nil {
		t.Fatal("expected text diff for added clause-1.2")
	}
	if addedText.NewContent != "Пункт два." {
		t.Errorf("NewContent = %q", addedText.NewContent)
	}
}

func TestComparer_RemovedClause(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Пункт один."),
			makeClause(t, "clause-1.2", "1.2", "Пункт два."),
		),
	))

	targetTree := makeTree(t, "doc-2", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Пункт один."),
		),
	))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	removedStruct := findStructDiff(result.StructuralDiffs, "clause-1.2")
	if removedStruct == nil {
		t.Fatal("expected structural diff for clause-1.2")
	}
	if removedStruct.Type != model.DiffTypeRemoved {
		t.Errorf("Type = %q, want %q", removedStruct.Type, model.DiffTypeRemoved)
	}

	removedText := findTextDiffByPath(result.TextDiffs, "Раздел 1 / Пункт 1.2", model.DiffTypeRemoved)
	if removedText == nil {
		t.Fatal("expected text diff for removed clause-1.2")
	}
	if removedText.OldContent != "Пункт два." {
		t.Errorf("OldContent = %q", removedText.OldContent)
	}
}

func TestComparer_AddedAppendix(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет"),
	))

	targetTree := makeTree(t, "doc-2", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет"),
		makeAppendix(t, "appendix-1", "1", "Спецификация",
			makeTextNode(t, "text-1", "Содержимое приложения."),
		),
	))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	addedApp := findStructDiff(result.StructuralDiffs, "appendix-1")
	if addedApp == nil {
		t.Fatal("expected structural diff for appendix-1")
	}
	if addedApp.Type != model.DiffTypeAdded {
		t.Errorf("Type = %q, want %q", addedApp.Type, model.DiffTypeAdded)
	}

	// Text node inside the appendix should also be added.
	addedText := findStructDiff(result.StructuralDiffs, "text-1")
	if addedText == nil {
		t.Fatal("expected structural diff for text-1")
	}
	if addedText.Type != model.DiffTypeAdded {
		t.Errorf("Type = %q, want %q", addedText.Type, model.DiffTypeAdded)
	}
}

func TestComparer_MovedNode(t *testing.T) {
	c := NewComparer()

	// clause-1.1 is under section-1 in base.
	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makeSection(t, "section-1", "1", "Раздел 1",
			makeClause(t, "clause-1.1", "1.1", "Пункт."),
		),
		makeSection(t, "section-2", "2", "Раздел 2"),
	))

	// clause-1.1 is under section-2 in target.
	targetTree := makeTree(t, "doc-2", makeRoot(t,
		makeSection(t, "section-1", "1", "Раздел 1"),
		makeSection(t, "section-2", "2", "Раздел 2",
			makeClause(t, "clause-1.1", "1.1", "Пункт."),
		),
	))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	movedDiff := findStructDiff(result.StructuralDiffs, "clause-1.1")
	if movedDiff == nil {
		t.Fatal("expected structural diff for clause-1.1")
	}
	if movedDiff.Type != model.DiffTypeModified {
		t.Errorf("Type = %q, want %q", movedDiff.Type, model.DiffTypeModified)
	}
	if movedDiff.Description != "узел перемещён" {
		t.Errorf("Description = %q, want %q", movedDiff.Description, "узел перемещён")
	}

	// No text diffs — content is the same.
	if len(result.TextDiffs) != 0 {
		t.Errorf("TextDiffs = %d, want 0", len(result.TextDiffs))
	}
}

func TestComparer_EmptyBaseTree(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "doc-1", makeRoot(t)) // root with no children

	targetTree := makeTree(t, "doc-2", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Текст пункта."),
		),
	))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	// All non-root nodes in target should be added.
	for _, sd := range result.StructuralDiffs {
		if sd.Type != model.DiffTypeAdded {
			t.Errorf("StructuralDiff %q: Type = %q, want %q", sd.NodeID, sd.Type, model.DiffTypeAdded)
		}
	}
	if len(result.StructuralDiffs) != 2 {
		t.Errorf("StructuralDiffs = %d, want 2 (section + clause)", len(result.StructuralDiffs))
	}
}

func TestComparer_EmptyTargetTree(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Текст пункта."),
		),
	))

	targetTree := makeTree(t, "doc-2", makeRoot(t)) // root with no children

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	// All non-root nodes in base should be removed.
	for _, sd := range result.StructuralDiffs {
		if sd.Type != model.DiffTypeRemoved {
			t.Errorf("StructuralDiff %q: Type = %q, want %q", sd.NodeID, sd.Type, model.DiffTypeRemoved)
		}
	}
	if len(result.StructuralDiffs) != 2 {
		t.Errorf("StructuralDiffs = %d, want 2 (section + clause)", len(result.StructuralDiffs))
	}
}

func TestComparer_BothEmpty(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "doc-1", makeRoot(t))
	targetTree := makeTree(t, "doc-2", makeRoot(t))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	if len(result.TextDiffs) != 0 {
		t.Errorf("TextDiffs = %d, want 0", len(result.TextDiffs))
	}
	if len(result.StructuralDiffs) != 0 {
		t.Errorf("StructuralDiffs = %d, want 0", len(result.StructuralDiffs))
	}
}

func TestComparer_NilBaseTree(t *testing.T) {
	c := NewComparer()
	targetTree := makeTree(t, "doc-2", makeRoot(t))

	result, err := c.Compare(context.Background(), nil, targetTree)
	assertExtractionError(t, err)
	if result != nil {
		t.Error("result should be nil on error")
	}
}

func TestComparer_NilTargetTree(t *testing.T) {
	c := NewComparer()
	baseTree := makeTree(t, "doc-1", makeRoot(t))

	result, err := c.Compare(context.Background(), baseTree, nil)
	assertExtractionError(t, err)
	if result != nil {
		t.Error("result should be nil on error")
	}
}

func TestComparer_ContextCancelled(t *testing.T) {
	c := NewComparer()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	baseTree := makeTree(t, "doc-1", makeRoot(t))
	targetTree := makeTree(t, "doc-2", makeRoot(t))

	result, err := c.Compare(ctx, baseTree, targetTree)
	assertExtractionError(t, err)
	if result != nil {
		t.Error("result should be nil on error")
	}
}

func TestComparer_PathStrings(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Текст.",
				makeClause(t, "subclause-1.1.1", "1.1.1", "Подпункт."),
			),
		),
		makeAppendix(t, "appendix-1", "1", "Спецификация",
			makeTextNode(t, "text-1", "Содержание."),
		),
		makePartyDetails(t, "party-1", "ООО «Ромашка»", nil),
	))

	// Target is empty — all nodes removed, so we can inspect paths on diffs.
	targetTree := makeTree(t, "doc-2", makeRoot(t))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	tests := []struct {
		nodeID   string
		wantPath string
	}{
		{"section-1", "Раздел 1"},
		{"clause-1.1", "Раздел 1 / Пункт 1.1"},
		{"subclause-1.1.1", "Раздел 1 / Пункт 1.1 / Пункт 1.1.1"},
		{"appendix-1", "Приложение 1"},
		{"text-1", "Приложение 1 / Текст"},
		{"party-1", "Реквизиты: ООО «Ромашка»"},
	}

	for _, tt := range tests {
		t.Run(tt.nodeID, func(t *testing.T) {
			sd := findStructDiff(result.StructuralDiffs, tt.nodeID)
			if sd == nil {
				t.Fatalf("no structural diff for %q", tt.nodeID)
			}
			if sd.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", sd.Path, tt.wantPath)
			}
		})
	}
}

func TestComparer_MultipleChanges(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Исполнитель обязуется."),
			makeClause(t, "clause-1.2", "1.2", "Заказчик обязуется."),
		),
		makeSection(t, "section-2", "2", "Стоимость",
			makeClause(t, "clause-2.1", "2.1", "Стоимость 100 000 руб."),
		),
	))

	targetTree := makeTree(t, "doc-2", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Исполнитель обязуется."),
			// clause-1.2 removed
		),
		makeSection(t, "section-2", "2", "Стоимость",
			makeClause(t, "clause-2.1", "2.1", "Стоимость 200 000 руб."), // modified
		),
		makeSection(t, "section-3", "3", "Сроки"), // added
	))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	// Structural: clause-1.2 removed, section-3 added.
	removedStruct := findStructDiff(result.StructuralDiffs, "clause-1.2")
	if removedStruct == nil || removedStruct.Type != model.DiffTypeRemoved {
		t.Error("expected clause-1.2 removed")
	}
	addedStruct := findStructDiff(result.StructuralDiffs, "section-3")
	if addedStruct == nil || addedStruct.Type != model.DiffTypeAdded {
		t.Error("expected section-3 added")
	}

	// Text: clause-1.2 removed, clause-2.1 modified.
	removedText := findTextDiffByPath(result.TextDiffs, "Раздел 1 / Пункт 1.2", model.DiffTypeRemoved)
	if removedText == nil {
		t.Error("expected text diff for removed clause-1.2")
	}
	modifiedText := findTextDiffByPath(result.TextDiffs, "Раздел 2 / Пункт 2.1", model.DiffTypeModified)
	if modifiedText == nil {
		t.Fatal("expected text diff for modified clause-2.1")
	}
	if modifiedText.OldContent != "Стоимость 100 000 руб." {
		t.Errorf("OldContent = %q", modifiedText.OldContent)
	}
	if modifiedText.NewContent != "Стоимость 200 000 руб." {
		t.Errorf("NewContent = %q", modifiedText.NewContent)
	}
}

func TestComparer_PartyDetailsChanged(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makePartyDetails(t, "party-1", "ООО «Ромашка»", map[string]string{
			"inn": "7701234567",
		}),
	))

	targetTree := makeTree(t, "doc-2", makeRoot(t,
		makePartyDetails(t, "party-1", "ООО «Ромашка» (новое название)", map[string]string{
			"inn": "7701234567",
		}),
	))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	// Content changed → text diff modified.
	if len(result.TextDiffs) != 1 {
		t.Fatalf("TextDiffs = %d, want 1", len(result.TextDiffs))
	}
	td := result.TextDiffs[0]
	if td.Type != model.DiffTypeModified {
		t.Errorf("Type = %q, want %q", td.Type, model.DiffTypeModified)
	}
	if td.OldContent != "ООО «Ромашка»" {
		t.Errorf("OldContent = %q", td.OldContent)
	}
	if td.NewContent != "ООО «Ромашка» (новое название)" {
		t.Errorf("NewContent = %q", td.NewContent)
	}
}

func TestComparer_AppendixContentModified(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makeAppendix(t, "appendix-1", "1", "Спецификация",
			makeTextNode(t, "text-1", "Старое содержимое приложения."),
		),
	))

	targetTree := makeTree(t, "doc-2", makeRoot(t,
		makeAppendix(t, "appendix-1", "1", "Спецификация",
			makeTextNode(t, "text-1", "Новое содержимое приложения."),
		),
	))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	// No structural diffs — same structure.
	if len(result.StructuralDiffs) != 0 {
		t.Errorf("StructuralDiffs = %d, want 0", len(result.StructuralDiffs))
	}

	// Text diff for the text node inside appendix.
	if len(result.TextDiffs) != 1 {
		t.Fatalf("TextDiffs = %d, want 1", len(result.TextDiffs))
	}
	td := result.TextDiffs[0]
	if td.Type != model.DiffTypeModified {
		t.Errorf("Type = %q, want %q", td.Type, model.DiffTypeModified)
	}
	if td.OldContent != "Старое содержимое приложения." {
		t.Errorf("OldContent = %q", td.OldContent)
	}
	if td.NewContent != "Новое содержимое приложения." {
		t.Errorf("NewContent = %q", td.NewContent)
	}
}

func TestComparer_StructuralOnlyChange(t *testing.T) {
	c := NewComparer()

	// clause-1.1 is child 0 of section-1 in base.
	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Текст пункта."),
			makeClause(t, "clause-1.2", "1.2", "Второй пункт."),
		),
	))

	// Swapped order: clause-1.2 is child 0, clause-1.1 is child 1 in target.
	targetTree := makeTree(t, "doc-2", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.2", "1.2", "Второй пункт."),
			makeClause(t, "clause-1.1", "1.1", "Текст пункта."),
		),
	))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	// Structural: both clauses moved (childIdx changed).
	if len(result.StructuralDiffs) != 2 {
		t.Fatalf("StructuralDiffs = %d, want 2", len(result.StructuralDiffs))
	}
	for _, sd := range result.StructuralDiffs {
		if sd.Type != model.DiffTypeModified {
			t.Errorf("NodeID %q: Type = %q, want %q", sd.NodeID, sd.Type, model.DiffTypeModified)
		}
		if sd.Description != "узел перемещён" {
			t.Errorf("NodeID %q: Description = %q", sd.NodeID, sd.Description)
		}
	}

	// No text diffs — content unchanged.
	if len(result.TextDiffs) != 0 {
		t.Errorf("TextDiffs = %d, want 0", len(result.TextDiffs))
	}
}

func TestComparer_TextOnlyChange(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Старый текст."),
		),
	))

	targetTree := makeTree(t, "doc-2", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Новый текст."),
		),
	))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	// No structural diffs.
	if len(result.StructuralDiffs) != 0 {
		t.Errorf("StructuralDiffs = %d, want 0", len(result.StructuralDiffs))
	}

	// One text modification.
	if len(result.TextDiffs) != 1 {
		t.Fatalf("TextDiffs = %d, want 1", len(result.TextDiffs))
	}
	td := result.TextDiffs[0]
	if td.Type != model.DiffTypeModified {
		t.Errorf("Type = %q, want modified", td.Type)
	}
	if td.OldContent != "Старый текст." {
		t.Errorf("OldContent = %q", td.OldContent)
	}
	if td.NewContent != "Новый текст." {
		t.Errorf("NewContent = %q", td.NewContent)
	}
}

func TestComparer_DocumentIDFromBaseTree(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "base-doc-id", makeRoot(t))
	targetTree := makeTree(t, "target-doc-id", makeRoot(t))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	if result.DocumentID != "base-doc-id" {
		t.Errorf("DocumentID = %q, want %q", result.DocumentID, "base-doc-id")
	}
	if result.BaseVersionID != "base-doc-id" {
		t.Errorf("BaseVersionID = %q, want %q", result.BaseVersionID, "base-doc-id")
	}
	if result.TargetVersionID != "target-doc-id" {
		t.Errorf("TargetVersionID = %q, want %q", result.TargetVersionID, "target-doc-id")
	}
}

func TestComparer_NilRootInBase(t *testing.T) {
	c := NewComparer()

	// Tree with nil root — represents a tree that exists but has no content.
	baseTree := &model.SemanticTree{DocumentID: "doc-1", Root: nil}
	targetTree := makeTree(t, "doc-2", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Текст."),
		),
	))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	// All target nodes added.
	if len(result.StructuralDiffs) != 2 {
		t.Errorf("StructuralDiffs = %d, want 2", len(result.StructuralDiffs))
	}
	for _, sd := range result.StructuralDiffs {
		if sd.Type != model.DiffTypeAdded {
			t.Errorf("Type = %q, want %q", sd.Type, model.DiffTypeAdded)
		}
	}
}

func TestComparer_NilRootInTarget(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Текст."),
		),
	))
	targetTree := &model.SemanticTree{DocumentID: "doc-2", Root: nil}

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	// All base nodes removed.
	if len(result.StructuralDiffs) != 2 {
		t.Errorf("StructuralDiffs = %d, want 2", len(result.StructuralDiffs))
	}
	for _, sd := range result.StructuralDiffs {
		if sd.Type != model.DiffTypeRemoved {
			t.Errorf("Type = %q, want %q", sd.Type, model.DiffTypeRemoved)
		}
	}
}

func TestComparer_EmptySlicesNotNil(t *testing.T) {
	c := NewComparer()

	baseTree := makeTree(t, "doc-1", makeRoot(t))
	targetTree := makeTree(t, "doc-2", makeRoot(t))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	// Verify slices are initialized (not nil) — important for JSON serialization.
	if result.TextDiffs == nil {
		t.Error("TextDiffs should be empty slice, not nil")
	}
	if result.StructuralDiffs == nil {
		t.Error("StructuralDiffs should be empty slice, not nil")
	}
}

func TestComparer_DeepNestedStructure(t *testing.T) {
	c := NewComparer()

	// Base: section -> clause -> subclause
	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Пункт.",
				makeClause(t, "subclause-1.1.1", "1.1.1", "Подпункт А."),
				makeClause(t, "subclause-1.1.2", "1.1.2", "Подпункт Б."),
			),
		),
	))

	// Target: one subclause removed, one modified
	targetTree := makeTree(t, "doc-2", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Пункт.",
				makeClause(t, "subclause-1.1.1", "1.1.1", "Подпункт А (изменён)."),
			),
		),
	))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	// subclause-1.1.2 removed structurally.
	removedStruct := findStructDiff(result.StructuralDiffs, "subclause-1.1.2")
	if removedStruct == nil || removedStruct.Type != model.DiffTypeRemoved {
		t.Error("expected subclause-1.1.2 removed structurally")
	}

	// subclause-1.1.1 content modified.
	modifiedText := findTextDiffByPath(result.TextDiffs, "Раздел 1 / Пункт 1.1 / Пункт 1.1.1", model.DiffTypeModified)
	if modifiedText == nil {
		t.Fatal("expected text diff for modified subclause-1.1.1")
	}
	if modifiedText.OldContent != "Подпункт А." {
		t.Errorf("OldContent = %q", modifiedText.OldContent)
	}
	if modifiedText.NewContent != "Подпункт А (изменён)." {
		t.Errorf("NewContent = %q", modifiedText.NewContent)
	}
}

func TestComparer_NodeLabelFallbacks(t *testing.T) {
	// Test nodeLabel with missing metadata.
	tests := []struct {
		name     string
		node     *model.SemanticNode
		wantPath string
	}{
		{
			name: "section with number",
			node: &model.SemanticNode{
				Type:     model.NodeTypeSection,
				Metadata: map[string]string{"number": "1", "title": "Предмет"},
			},
			wantPath: "Раздел 1",
		},
		{
			name: "section without number, with title",
			node: &model.SemanticNode{
				Type:     model.NodeTypeSection,
				Metadata: map[string]string{"title": "Общие положения"},
			},
			wantPath: "Раздел Общие положения",
		},
		{
			name: "section with empty metadata",
			node: &model.SemanticNode{
				Type:     model.NodeTypeSection,
				Metadata: map[string]string{},
			},
			wantPath: "Раздел",
		},
		{
			name: "party details with name",
			node: &model.SemanticNode{
				Type:     model.NodeTypePartyDetails,
				Content:  "ООО «Тест»",
				Metadata: map[string]string{"name": "ООО «Тест»"},
			},
			wantPath: "Реквизиты: ООО «Тест»",
		},
		{
			name: "party details without name in metadata, with content",
			node: &model.SemanticNode{
				Type:     model.NodeTypePartyDetails,
				Content:  "ИП Иванов",
				Metadata: map[string]string{},
			},
			wantPath: "Реквизиты: ИП Иванов",
		},
		{
			name: "text node",
			node: &model.SemanticNode{
				Type: model.NodeTypeText,
			},
			wantPath: "Текст",
		},
		{
			name: "root node",
			node: &model.SemanticNode{
				Type: model.NodeTypeRoot,
			},
			wantPath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nodeLabel(tt.node)
			if got != tt.wantPath {
				t.Errorf("nodeLabel() = %q, want %q", got, tt.wantPath)
			}
		})
	}
}

func TestComparer_ContentAppearsInExistingNode(t *testing.T) {
	c := NewComparer()

	// Base: clause exists but has empty content.
	baseTree := makeTree(t, "doc-1", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", ""),
		),
	))

	// Target: same clause now has content.
	targetTree := makeTree(t, "doc-2", makeRoot(t,
		makeSection(t, "section-1", "1", "Предмет",
			makeClause(t, "clause-1.1", "1.1", "Новый текст пункта."),
		),
	))

	result, err := c.Compare(context.Background(), baseTree, targetTree)
	assertNoError(t, err)

	// No structural diffs.
	if len(result.StructuralDiffs) != 0 {
		t.Errorf("StructuralDiffs = %d, want 0", len(result.StructuralDiffs))
	}

	// Text diff: modified (old empty -> new content).
	if len(result.TextDiffs) != 1 {
		t.Fatalf("TextDiffs = %d, want 1", len(result.TextDiffs))
	}
	td := result.TextDiffs[0]
	if td.Type != model.DiffTypeModified {
		t.Errorf("Type = %q, want %q", td.Type, model.DiffTypeModified)
	}
	if td.OldContent != "" {
		t.Errorf("OldContent = %q, want empty", td.OldContent)
	}
	if td.NewContent != "Новый текст пункта." {
		t.Errorf("NewContent = %q", td.NewContent)
	}
}
