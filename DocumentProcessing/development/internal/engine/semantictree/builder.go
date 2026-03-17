package semantictree

import (
	"context"
	"fmt"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// Builder implements SemanticTreeBuilderPort — transforms extracted text and
// document structure into a hierarchical semantic tree for downstream analysis.
type Builder struct{}

// NewBuilder creates a new semantic tree builder.
func NewBuilder() *Builder {
	return &Builder{}
}

// Build constructs a SemanticTree from the extracted text and document structure.
// The structure parameter drives the tree shape; text is informational and may be nil.
// Returns an ExtractionError if context is cancelled or structure is nil.
//
// NOTE(v1): the text parameter is accepted per the SemanticTreeBuilderPort contract
// but is not used in the current implementation. The tree is built entirely from
// DocumentStructure, which already contains all text content (section content,
// clause content, appendix content, party details). A future version may use
// ExtractedText to capture preamble text or other content not covered by the
// structure extractor.
func (b *Builder) Build(ctx context.Context, text *model.ExtractedText, structure *model.DocumentStructure) (*model.SemanticTree, error) {
	if err := ctx.Err(); err != nil {
		return nil, port.NewExtractionError("context cancelled before semantic tree building", err)
	}

	if structure == nil {
		return nil, port.NewExtractionError("document structure is nil", nil)
	}

	gen := &idGen{}

	root := &model.SemanticNode{
		ID:   "root",
		Type: model.NodeTypeRoot,
	}

	// Sections -> SectionNode children of root.
	for i, section := range structure.Sections {
		sectionNode := &model.SemanticNode{
			ID:   fmt.Sprintf("section-%d", i+1),
			Type: model.NodeTypeSection,
			Metadata: map[string]string{
				"number": section.Number,
				"title":  section.Title,
			},
		}

		// Section-level content becomes a TextNode child.
		if section.Content != "" {
			sectionNode.Children = append(sectionNode.Children, &model.SemanticNode{
				ID:      gen.nextText(),
				Type:    model.NodeTypeText,
				Content: section.Content,
			})
		}

		// Clauses -> ClauseNode children of SectionNode.
		for _, clause := range section.Clauses {
			clauseNode := &model.SemanticNode{
				ID:      fmt.Sprintf("clause-%s", clause.Number),
				Type:    model.NodeTypeClause,
				Content: clause.Content,
				Metadata: map[string]string{
					"number": clause.Number,
				},
			}

			// Sub-clauses -> ClauseNode children of ClauseNode.
			for _, sub := range clause.SubClauses {
				subNode := &model.SemanticNode{
					ID:      fmt.Sprintf("subclause-%s", sub.Number),
					Type:    model.NodeTypeClause,
					Content: sub.Content,
					Metadata: map[string]string{
						"number": sub.Number,
					},
				}
				clauseNode.Children = append(clauseNode.Children, subNode)
			}

			sectionNode.Children = append(sectionNode.Children, clauseNode)
		}

		root.Children = append(root.Children, sectionNode)
	}

	// Appendices -> AppendixNode children of root.
	for i, appendix := range structure.Appendices {
		appendixNode := &model.SemanticNode{
			ID:   fmt.Sprintf("appendix-%d", i+1),
			Type: model.NodeTypeAppendix,
			Metadata: map[string]string{
				"number": appendix.Number,
				"title":  appendix.Title,
			},
		}

		if appendix.Content != "" {
			appendixNode.Children = append(appendixNode.Children, &model.SemanticNode{
				ID:      gen.nextText(),
				Type:    model.NodeTypeText,
				Content: appendix.Content,
			})
		}

		root.Children = append(root.Children, appendixNode)
	}

	// Party details -> PartyDetailsNode children of root.
	for i, party := range structure.PartyDetails {
		partyNode := &model.SemanticNode{
			ID:      fmt.Sprintf("party-%d", i+1),
			Type:    model.NodeTypePartyDetails,
			Content: party.Name,
			Metadata: map[string]string{
				"name": party.Name,
			},
		}
		if party.INN != "" {
			partyNode.Metadata["inn"] = party.INN
		}
		if party.OGRN != "" {
			partyNode.Metadata["ogrn"] = party.OGRN
		}
		if party.Address != "" {
			partyNode.Metadata["address"] = party.Address
		}
		if party.Representative != "" {
			partyNode.Metadata["representative"] = party.Representative
		}

		root.Children = append(root.Children, partyNode)
	}

	return &model.SemanticTree{
		DocumentID: structure.DocumentID,
		Root:       root,
	}, nil
}

// idGen generates sequential IDs for text nodes.
type idGen struct {
	textCounter int
}

func (g *idGen) nextText() string {
	g.textCounter++
	return fmt.Sprintf("text-%d", g.textCounter)
}

// compile-time check: Builder implements SemanticTreeBuilderPort.
var _ port.SemanticTreeBuilderPort = (*Builder)(nil)
