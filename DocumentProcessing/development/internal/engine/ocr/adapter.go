package ocr

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"contractpro/document-processing/internal/application/warning"
	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// Warning code constants emitted by the OCR adapter.
const (
	WarnPartialRecognition = "OCR_PARTIAL_RECOGNITION"
	WarnLowQuality         = "OCR_LOW_QUALITY"
	lowQualityThreshold    = 50
)

// Adapter determines whether OCR is needed for a PDF and routes accordingly.
// For text-based PDFs, OCR is skipped (not_applicable).
// For scanned PDFs, the file is downloaded from temporary storage and sent to
// the external OCR service for recognition with rate limiting and retry logic.
type Adapter struct {
	ocrService  port.OCRServicePort
	storage     port.TempStoragePort
	warnings    *warning.Collector
	rpsLimit    int
	maxAttempts int
	backoffBase time.Duration

	// token bucket state
	mu         sync.Mutex
	tokens     float64
	capacity   float64
	lastRefill time.Time
}

// NewAdapter creates an Adapter with the given OCR service, temporary storage,
// warning collector, rate limit, max retry attempts, and backoff base duration.
//
// If rpsLimit <= 0, it defaults to 10. If maxAttempts <= 0, it defaults to 1.
func NewAdapter(
	ocrService port.OCRServicePort,
	storage port.TempStoragePort,
	warnings *warning.Collector,
	rpsLimit int,
	maxAttempts int,
	backoffBase time.Duration,
) *Adapter {
	if warnings == nil {
		panic("ocr.NewAdapter: warnings collector must not be nil")
	}
	if rpsLimit <= 0 {
		rpsLimit = 10
	}
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	return &Adapter{
		ocrService:  ocrService,
		storage:     storage,
		warnings:    warnings,
		rpsLimit:    rpsLimit,
		maxAttempts: maxAttempts,
		backoffBase: backoffBase,
		tokens:      float64(rpsLimit),
		capacity:    float64(rpsLimit),
		lastRefill:  time.Now(),
	}
}

// Process determines whether OCR is needed and performs it if necessary.
//
// If isTextPDF is true, the PDF already contains extractable text and OCR is
// skipped — returns an artifact with status not_applicable.
//
// If isTextPDF is false (scanned PDF), the file is downloaded from temporary
// storage using storageKey, sent to the OCR service for recognition with
// rate limiting and retry logic, and the raw text is returned with status
// applicable. Warnings are emitted for empty or low-quality recognition results.
func (a *Adapter) Process(ctx context.Context, storageKey string, isTextPDF bool) (*model.OCRRawArtifact, error) {
	if isTextPDF {
		return &model.OCRRawArtifact{Status: model.OCRStatusNotApplicable}, nil
	}

	reader, err := a.storage.Download(ctx, storageKey)
	if err != nil {
		return nil, port.NewStorageError("download PDF for OCR: "+err.Error(), err)
	}

	if err := ctx.Err(); err != nil {
		reader.Close()
		return nil, err
	}

	var lastErr error

	for attempt := 1; attempt <= a.maxAttempts; attempt++ {
		if err := a.acquireToken(ctx); err != nil {
			reader.Close()
			return nil, err
		}

		rawText, ocrErr := a.ocrService.Recognize(ctx, reader)
		if ocrErr == nil {
			reader.Close()
			a.checkWarnings(rawText)

			return &model.OCRRawArtifact{
				Status:  model.OCRStatusApplicable,
				RawText: rawText,
			}, nil
		}

		lastErr = ocrErr

		if !port.IsRetryable(ocrErr) {
			reader.Close()
			return nil, port.NewOCRError(ocrErr.Error(), false, ocrErr)
		}

		if attempt == a.maxAttempts {
			reader.Close()
			return nil, port.NewOCRError(
				fmt.Sprintf("OCR failed after %d attempts: %v", a.maxAttempts, lastErr),
				true,
				lastErr,
			)
		}

		// Close current reader and wait before re-downloading for next attempt.
		reader.Close()

		if err := a.backoff(ctx, attempt); err != nil {
			return nil, err
		}

		reader, err = a.storage.Download(ctx, storageKey)
		if err != nil {
			return nil, port.NewStorageError("re-download PDF for OCR retry: "+err.Error(), err)
		}
	}

	// Unreachable: the loop always returns. Guard for safety.
	return nil, port.NewOCRError(
		fmt.Sprintf("OCR failed after %d attempts: %v", a.maxAttempts, lastErr),
		true,
		lastErr,
	)
}

// checkWarnings adds OCR quality warnings to the collector. Warnings are
// mutually exclusive: empty/whitespace text triggers partial recognition,
// non-empty text shorter than lowQualityThreshold triggers low quality.
func (a *Adapter) checkWarnings(rawText string) {
	trimmed := strings.TrimSpace(rawText)

	if trimmed == "" {
		a.warnings.Add(model.ProcessingWarning{
			Code:    WarnPartialRecognition,
			Message: "OCR produced empty or whitespace-only text",
			Stage:   model.ProcessingStageOCR,
		})
		return
	}

	if len([]rune(trimmed)) < lowQualityThreshold {
		a.warnings.Add(model.ProcessingWarning{
			Code:    WarnLowQuality,
			Message: "OCR produced very short text, possible low quality scan",
			Stage:   model.ProcessingStageOCR,
		})
	}
}

// refill adds tokens based on elapsed time since last refill. Must be called
// under mu lock.
func (a *Adapter) refill() {
	now := time.Now()
	elapsed := now.Sub(a.lastRefill).Seconds()
	a.tokens = math.Min(a.capacity, a.tokens+elapsed*a.capacity)
	a.lastRefill = now
}

// acquireToken blocks until a rate limit token is available or the context is
// cancelled. Returns nil on success, ctx.Err() on cancellation.
func (a *Adapter) acquireToken(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		a.mu.Lock()
		a.refill()

		if a.tokens >= 1.0 {
			a.tokens -= 1.0
			a.mu.Unlock()
			return nil
		}

		deficit := 1.0 - a.tokens
		waitDur := time.Duration(deficit / a.capacity * float64(time.Second))
		a.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDur):
			// try again
		}
	}
}

// backoff waits for an exponential backoff duration based on the attempt number.
// Returns nil when the wait completes, or ctx.Err() if the context is cancelled.
func (a *Adapter) backoff(ctx context.Context, attempt int) error {
	shift := attempt - 1
	if shift > 30 {
		shift = 30
	}
	dur := a.backoffBase * time.Duration(1<<uint(shift))
	select {
	case <-time.After(dur):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
