package comparison

import (
	"context"
	"sort"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// Comparer implements VersionComparisonPort — compares two document versions
// by their semantic trees, producing text-level and structural diffs
// (FR-5.3.1, FR-5.3.2).
type Comparer struct{}

// NewComparer creates a new version comparison engine.
func NewComparer() *Comparer {
	return &Comparer{}
}

// nodeInfo holds indexed information about a single node in the semantic tree.
type nodeInfo struct {
	node     *model.SemanticNode
	path     string // human-readable path, e.g. "Раздел 1 / Пункт 1.1"
	parentID string
	childIdx int // position among siblings
}

// Compare compares two semantic trees and produces text-level and structural diffs.
// Returns an ExtractionError if context is cancelled or either tree is nil.
func (c *Comparer) Compare(ctx context.Context, baseTree, targetTree *model.SemanticTree) (*model.VersionDiffResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, port.NewExtractionError("context cancelled before version comparison", err)
	}

	if baseTree == nil {
		return nil, port.NewExtractionError("base tree is nil", nil)
	}

	if targetTree == nil {
		return nil, port.NewExtractionError("target tree is nil", nil)
	}

	baseIndex := buildIndex(baseTree)
	targetIndex := buildIndex(targetTree)

	structDiffs := computeStructuralDiffs(baseIndex, targetIndex)
	textDiffs := computeTextDiffs(baseIndex, targetIndex)

	return &model.VersionDiffResult{
		DocumentID:      baseTree.DocumentID,
		BaseVersionID:   baseTree.DocumentID,
		TargetVersionID: targetTree.DocumentID,
		TextDiffs:       textDiffs,
		StructuralDiffs: structDiffs,
	}, nil
}

// buildIndex walks the semantic tree DFS and builds a map from node ID to nodeInfo.
func buildIndex(tree *model.SemanticTree) map[string]*nodeInfo {
	index := make(map[string]*nodeInfo)
	if tree.Root == nil {
		return index
	}
	buildIndexRecursive(tree.Root, "", "", 0, index)
	return index
}

// buildIndexRecursive recursively indexes nodes, tracking parent path, parent ID, and child position.
func buildIndexRecursive(node *model.SemanticNode, parentPath string, parentID string, childIdx int, index map[string]*nodeInfo) {
	segment := nodeLabel(node)

	var path string
	if segment == "" {
		// ROOT node — omit from path.
		path = parentPath
	} else if parentPath == "" {
		path = segment
	} else {
		path = parentPath + " / " + segment
	}

	index[node.ID] = &nodeInfo{
		node:     node,
		path:     path,
		parentID: parentID,
		childIdx: childIdx,
	}

	for i, child := range node.Children {
		buildIndexRecursive(child, path, node.ID, i, index)
	}
}

// nodeLabel builds a single path segment from node type and metadata.
func nodeLabel(node *model.SemanticNode) string {
	switch node.Type {
	case model.NodeTypeRoot:
		return ""
	case model.NodeTypeSection:
		number := node.Metadata["number"]
		if number != "" {
			return "Раздел " + number
		}
		title := node.Metadata["title"]
		if title != "" {
			return "Раздел " + title
		}
		return "Раздел"
	case model.NodeTypeClause:
		number := node.Metadata["number"]
		if number != "" {
			return "Пункт " + number
		}
		return "Пункт"
	case model.NodeTypeAppendix:
		number := node.Metadata["number"]
		if number != "" {
			return "Приложение " + number
		}
		return "Приложение"
	case model.NodeTypePartyDetails:
		name := node.Metadata["name"]
		if name != "" {
			return "Реквизиты: " + name
		}
		if node.Content != "" {
			return "Реквизиты: " + node.Content
		}
		return "Реквизиты"
	case model.NodeTypeText:
		return "Текст"
	default:
		return string(node.Type)
	}
}

// computeStructuralDiffs performs three passes over base and target indexes:
// 1. Nodes in base not in target → removed
// 2. Nodes in target not in base → added
// 3. Nodes in both but moved (different parentID or childIdx) → modified
func computeStructuralDiffs(baseIndex, targetIndex map[string]*nodeInfo) []model.StructuralDiffEntry {
	diffs := []model.StructuralDiffEntry{}

	// Pass 1: removed nodes (in base, not in target).
	for id, info := range baseIndex {
		if info.node.Type == model.NodeTypeRoot {
			continue
		}
		if _, ok := targetIndex[id]; !ok {
			diffs = append(diffs, model.StructuralDiffEntry{
				Type:        model.DiffTypeRemoved,
				NodeType:    info.node.Type,
				NodeID:      id,
				Path:        info.path,
				Description: "узел удалён",
			})
		}
	}

	// Pass 2: added nodes (in target, not in base).
	for id, info := range targetIndex {
		if info.node.Type == model.NodeTypeRoot {
			continue
		}
		if _, ok := baseIndex[id]; !ok {
			diffs = append(diffs, model.StructuralDiffEntry{
				Type:        model.DiffTypeAdded,
				NodeType:    info.node.Type,
				NodeID:      id,
				Path:        info.path,
				Description: "узел добавлен",
			})
		}
	}

	// Pass 3: moved nodes (in both, but parentID or childIdx changed).
	for id, baseInfo := range baseIndex {
		if baseInfo.node.Type == model.NodeTypeRoot {
			continue
		}
		targetInfo, ok := targetIndex[id]
		if !ok {
			continue
		}
		if baseInfo.parentID != targetInfo.parentID || baseInfo.childIdx != targetInfo.childIdx {
			diffs = append(diffs, model.StructuralDiffEntry{
				Type:        model.DiffTypeModified,
				NodeType:    baseInfo.node.Type,
				NodeID:      id,
				Path:        baseInfo.path,
				Description: "узел перемещён",
			})
		}
	}

	// Sort deterministically by NodeID for stable output.
	sort.Slice(diffs, func(i, j int) bool {
		if diffs[i].Type != diffs[j].Type {
			return diffs[i].Type < diffs[j].Type
		}
		return diffs[i].NodeID < diffs[j].NodeID
	})

	return diffs
}

// computeTextDiffs compares content of nodes present in both trees, and reports
// content additions/removals for nodes unique to one tree.
func computeTextDiffs(baseIndex, targetIndex map[string]*nodeInfo) []model.TextDiffEntry {
	diffs := []model.TextDiffEntry{}

	// Nodes removed from base (content-bearing only).
	for id, info := range baseIndex {
		if info.node.Content == "" {
			continue
		}
		if _, ok := targetIndex[id]; !ok {
			diffs = append(diffs, model.TextDiffEntry{
				Type:       model.DiffTypeRemoved,
				Path:       info.path,
				OldContent: info.node.Content,
			})
		}
	}

	// Nodes added in target (content-bearing only).
	for id, info := range targetIndex {
		if info.node.Content == "" {
			continue
		}
		if _, ok := baseIndex[id]; !ok {
			diffs = append(diffs, model.TextDiffEntry{
				Type:       model.DiffTypeAdded,
				Path:       info.path,
				NewContent: info.node.Content,
			})
		}
	}

	// Nodes in both with changed content.
	for id, baseInfo := range baseIndex {
		targetInfo, ok := targetIndex[id]
		if !ok {
			continue
		}
		if baseInfo.node.Content == "" && targetInfo.node.Content == "" {
			continue
		}
		if baseInfo.node.Content != targetInfo.node.Content {
			diffs = append(diffs, model.TextDiffEntry{
				Type:       model.DiffTypeModified,
				Path:       baseInfo.path,
				OldContent: baseInfo.node.Content,
				NewContent: targetInfo.node.Content,
			})
		}
	}

	// Sort deterministically by Path then Type for stable output.
	sort.Slice(diffs, func(i, j int) bool {
		if diffs[i].Type != diffs[j].Type {
			return diffs[i].Type < diffs[j].Type
		}
		return diffs[i].Path < diffs[j].Path
	})

	return diffs
}

// compile-time check: Comparer implements VersionComparisonPort.
var _ port.VersionComparisonPort = (*Comparer)(nil)
