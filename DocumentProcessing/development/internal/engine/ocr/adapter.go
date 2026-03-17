package ocr

import (
	"context"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// Adapter determines whether OCR is needed for a PDF and routes accordingly.
// For text-based PDFs, OCR is skipped (not_applicable).
// For scanned PDFs, the file is downloaded from temporary storage and sent to
// the external OCR service for recognition.
type Adapter struct {
	ocrService port.OCRServicePort
	storage    port.TempStoragePort
}

// NewAdapter creates an Adapter with the given OCR service and temporary storage
// dependencies.
func NewAdapter(ocrService port.OCRServicePort, storage port.TempStoragePort) *Adapter {
	return &Adapter{
		ocrService: ocrService,
		storage:    storage,
	}
}

// Process determines whether OCR is needed and performs it if necessary.
//
// If isTextPDF is true, the PDF already contains extractable text and OCR is
// skipped — returns an artifact with status not_applicable.
//
// If isTextPDF is false (scanned PDF), the file is downloaded from temporary
// storage using storageKey, sent to the OCR service for recognition, and the
// raw text is returned with status applicable.
func (a *Adapter) Process(ctx context.Context, storageKey string, isTextPDF bool) (*model.OCRRawArtifact, error) {
	if isTextPDF {
		return &model.OCRRawArtifact{Status: model.OCRStatusNotApplicable}, nil
	}

	reader, err := a.storage.Download(ctx, storageKey)
	if err != nil {
		return nil, port.NewStorageError("download PDF for OCR: "+err.Error(), err)
	}
	defer reader.Close()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rawText, err := a.ocrService.Recognize(ctx, reader)
	if err != nil {
		return nil, port.NewOCRError(err.Error(), port.IsRetryable(err), err)
	}

	return &model.OCRRawArtifact{
		Status:  model.OCRStatusApplicable,
		RawText: rawText,
	}, nil
}
