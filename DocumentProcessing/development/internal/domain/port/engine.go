package port

import (
	"context"

	"contractpro/document-processing/internal/domain/model"
)

// FetchResult contains the result of downloading and validating a source file.
// Returned by SourceFileFetcherPort as part of the port contract.
type FetchResult struct {
	StorageKey string // key in temporary storage
	PageCount  int    // number of pages in the document
	IsTextPDF  bool   // true = text-based PDF, false = scanned PDF
	FileSize   int64  // actual file size in bytes
}

// InputValidatorPort validates command metadata before file download (FR-1.1.1).
// Checks: declared file size ≤ 20 MB, mime-type = application/pdf, required fields.
// Implemented by: Input Validator (engine layer).
// Used by: Processing Pipeline Orchestrator.
type InputValidatorPort interface {
	Validate(ctx context.Context, cmd model.ProcessDocumentCommand) error
}

// SourceFileFetcherPort downloads a PDF by URL, validates format/size/pages,
// and saves the file to temporary storage.
// Implemented by: Source File Fetcher (engine layer).
// Used by: Processing Pipeline Orchestrator.
type SourceFileFetcherPort interface {
	Fetch(ctx context.Context, cmd model.ProcessDocumentCommand) (*FetchResult, error)
}

// TextExtractionPort extracts and normalizes text from a PDF or OCR result (FR-1.3.1).
// If ocrResult is nil or has status not_applicable → extracts from PDF in temp storage.
// If ocrResult has status applicable → extracts from OCR raw text.
// Implemented by: Text Extraction & Normalization Engine (engine layer).
// Used by: Processing Pipeline Orchestrator.
type TextExtractionPort interface {
	Extract(ctx context.Context, storageKey string, ocrResult *model.OCRRawArtifact) (*model.ExtractedText, []model.ProcessingWarning, error)
}

// StructureExtractionPort extracts logical document structure: sections, clauses,
// sub-clauses, appendices, party details (FR-1.3.1, FR-1.3.2).
// Implemented by: Structure Extraction Engine (engine layer).
// Used by: Processing Pipeline Orchestrator.
type StructureExtractionPort interface {
	Extract(ctx context.Context, text *model.ExtractedText) (*model.DocumentStructure, []model.ProcessingWarning, error)
}

// SemanticTreeBuilderPort builds a semantic tree from extracted text and document structure.
// Implemented by: Semantic Tree Builder (engine layer).
// Used by: Processing Pipeline Orchestrator.
type SemanticTreeBuilderPort interface {
	Build(ctx context.Context, text *model.ExtractedText, structure *model.DocumentStructure) (*model.SemanticTree, error)
}

// VersionComparisonPort compares two document versions by their semantic trees,
// producing text-level and structural diffs (FR-5.3.1, FR-5.3.2).
// Implemented by: Version Comparison Engine (engine layer).
// Used by: Comparison Pipeline Orchestrator.
type VersionComparisonPort interface {
	Compare(ctx context.Context, baseTree, targetTree *model.SemanticTree) (*model.VersionDiffResult, error)
}
