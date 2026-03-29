# Engine Layer — CLAUDE.md

Stateless business logic components. Each implements a port interface from `domain/port`.

## Components

**validator/** — InputValidatorPort
- Validates file size ≤ 20MB, MIME type = application/pdf, required fields
- Includes SSRF protection (ssrf.go) for URL validation
- Fast-fail on invalid input before pipeline starts

**fetcher/** — SourceFileFetcherPort
- Downloads PDF by URL via HTTP
- Validates format, size, page count
- Saves to temp storage via TempStoragePort
- Uses SourceFileDownloaderPort for HTTP, retryable on transient errors

**ocr/** — OCRProcessorPort
- Routes text PDFs (skip OCR) vs scanned PDFs (send to Yandex Cloud Vision)
- Rate-limited with RPS controls
- Retryable on service errors

**textextract/** — TextExtractionPort
- Extracts text from PDF or OCR result
- Applies Unicode NFC normalization + garbage character removal (C0/C1 control, zero-width, BOM)
- Deterministic output for same input

**structure/** — StructureExtractionPort
- Regex-based extraction of Russian legal document structure
- Identifies: Раздел (sections), Пункт (clauses), Подпункт (subclauses), Приложение (appendices), Реквизиты (party details)
- Preserves location info for diff matching

**semantictree/** — SemanticTreeBuilderPort
- Builds SemanticTree from ExtractedText + DocumentStructure
- Assigns unique node IDs for diff matching
- Validates tree consistency

**comparison/** — VersionComparisonPort
- Three-pass diff algorithm: removed nodes → added nodes → modified/moved nodes
- ID-based node matching across versions
- Deterministic output (sorted by Type+NodeID/Path)
- Returns TextDiff + StructuralDiff

## Patterns

- NewComponentName() constructor for all components
- Compile-time interface checks: `var _ Port = (*Impl)(nil)`
- Error handling via domain/port.DomainError
- No mutable state; suitable for concurrent use
