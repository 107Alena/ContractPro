package model

// DiffType represents the type of a diff entry.
type DiffType string

const (
	DiffTypeAdded    DiffType = "added"
	DiffTypeRemoved  DiffType = "removed"
	DiffTypeModified DiffType = "modified"
)

// TextDiffEntry represents a single text-level difference between two document versions.
type TextDiffEntry struct {
	Type       DiffType `json:"type"`
	Path       string   `json:"path,omitempty"`
	OldContent string   `json:"old_content,omitempty"`
	NewContent string   `json:"new_content,omitempty"`
}

// StructuralDiffEntry represents a single structural difference based on semantic tree comparison.
type StructuralDiffEntry struct {
	Type        DiffType `json:"type"`
	NodeType    NodeType `json:"node_type"`
	NodeID      string   `json:"node_id"`
	Path        string   `json:"path,omitempty"`
	Description string   `json:"description,omitempty"`
}

// VersionDiffResult holds the complete result of comparing two document versions.
type VersionDiffResult struct {
	DocumentID      string                `json:"document_id"`
	BaseVersionID   string                `json:"base_version_id"`
	TargetVersionID string                `json:"target_version_id"`
	TextDiffs       []TextDiffEntry       `json:"text_diffs"`
	StructuralDiffs []StructuralDiffEntry `json:"structural_diffs"`
}
