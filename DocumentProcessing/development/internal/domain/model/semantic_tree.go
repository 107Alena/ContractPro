package model

// NodeType represents the type of a node in the semantic tree.
type NodeType string

const (
	NodeTypeRoot         NodeType = "ROOT"
	NodeTypeSection      NodeType = "SECTION"
	NodeTypeClause       NodeType = "CLAUSE"
	NodeTypeText         NodeType = "TEXT"
	NodeTypeAppendix     NodeType = "APPENDIX"
	NodeTypePartyDetails NodeType = "PARTY_DETAILS"
)

// SemanticNode represents a single node in the semantic tree.
type SemanticNode struct {
	ID       string            `json:"id"`
	Type     NodeType          `json:"type"`
	Content  string            `json:"content,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Children []*SemanticNode   `json:"children,omitempty"`
}

// SemanticTree represents the full semantic tree of a document.
type SemanticTree struct {
	DocumentID string        `json:"document_id"`
	Root       *SemanticNode `json:"root"`
}

// Walk performs a depth-first pre-order traversal of the tree.
// The callback fn is called for each node. If fn returns false, traversal stops.
// Walk returns false if traversal was stopped early.
func (t *SemanticTree) Walk(fn func(*SemanticNode) bool) bool {
	if t.Root == nil {
		return true
	}
	return walkNode(t.Root, fn)
}

func walkNode(n *SemanticNode, fn func(*SemanticNode) bool) bool {
	if !fn(n) {
		return false
	}
	for _, child := range n.Children {
		if !walkNode(child, fn) {
			return false
		}
	}
	return true
}
