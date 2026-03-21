package fetcher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
	pdfpkg "contractpro/document-processing/internal/pdf"
)

// PDFAnalyzer provides PDF format validation and metadata extraction.
// This is a consumer-side interface — the fetcher depends on the behavior,
// not on the concrete pdf.Util type.
type PDFAnalyzer interface {
	IsValidPDF(r io.Reader) bool
	Analyze(r io.ReadSeeker) (*pdfpkg.Info, error)
}

// Fetcher implements SourceFileFetcherPort — downloads a PDF by URL,
// validates file size, PDF format, and page count, then saves the file
// to temporary storage.
type Fetcher struct {
	downloader  port.SourceFileDownloaderPort
	storage     port.TempStoragePort
	pdfAnalyzer PDFAnalyzer
	maxFileSize int64
	maxPages    int
}

// NewFetcher creates a Fetcher with the given dependencies.
// maxFileSize is the maximum allowed file size in bytes (typically 20 MB).
// maxPages is the maximum allowed page count (typically 100).
func NewFetcher(
	downloader port.SourceFileDownloaderPort,
	storage port.TempStoragePort,
	pdfAnalyzer PDFAnalyzer,
	maxFileSize int64,
	maxPages int,
) *Fetcher {
	return &Fetcher{
		downloader:  downloader,
		storage:     storage,
		pdfAnalyzer: pdfAnalyzer,
		maxFileSize: maxFileSize,
		maxPages:    maxPages,
	}
}

// Fetch downloads the source file from cmd.FileURL, validates its size,
// PDF format, and page count, then uploads it to temporary storage under
// key "{job_id}/source.pdf".
// Returns a FetchResult with the storage key, actual file size, page count,
// and text/scan classification.
func (f *Fetcher) Fetch(ctx context.Context, cmd model.ProcessDocumentCommand) (*port.FetchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	body, contentLength, err := f.downloader.Download(ctx, cmd.FileURL)
	if err != nil {
		return nil, classifyDownloadError(err)
	}
	defer body.Close()

	// Early reject: if Content-Length is known and exceeds limit, don't read the body.
	if contentLength > 0 && contentLength > f.maxFileSize {
		return nil, port.NewFileTooLargeError(
			fmt.Sprintf("content-length %d bytes exceeds limit %d bytes", contentLength, f.maxFileSize),
		)
	}

	// Stream the download through limitedReader into an in-memory buffer.
	// Max file size is 20 MB so buffering is safe.
	limited := &limitedReader{
		r:     body,
		limit: f.maxFileSize,
	}

	var buf bytes.Buffer
	if contentLength > 0 && contentLength <= f.maxFileSize {
		buf.Grow(int(contentLength))
	}
	if _, err := io.Copy(&buf, limited); err != nil {
		// Pass through context errors raw (consistent with classifyDownloadError).
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, port.NewServiceUnavailableError("failed to read source file", err)
	}

	// If size was exceeded during streaming, reject without uploading.
	if limited.exceeded {
		return nil, port.NewFileTooLargeError(
			fmt.Sprintf("file size exceeds limit %d bytes during streaming", f.maxFileSize),
		)
	}

	// Validate PDF format (magic bytes check).
	reader := bytes.NewReader(buf.Bytes())
	if !f.pdfAnalyzer.IsValidPDF(reader) {
		return nil, port.NewInvalidFormatError("file is not a valid PDF")
	}

	// Analyze PDF: page count and text/scan classification.
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return nil, port.NewInvalidFormatError(
			fmt.Sprintf("failed to seek PDF reader: %v", err),
		)
	}
	info, err := f.pdfAnalyzer.Analyze(reader)
	if err != nil {
		return nil, port.NewInvalidFormatError(
			fmt.Sprintf("failed to analyze PDF: %v", err),
		)
	}

	// Validate page count.
	if info.PageCount > f.maxPages {
		return nil, port.NewTooManyPagesError(
			fmt.Sprintf("page count %d exceeds limit %d", info.PageCount, f.maxPages),
		)
	}

	// Upload validated content to temporary storage.
	storageKey := fmt.Sprintf("%s/source.pdf", cmd.JobID)
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return nil, port.NewServiceUnavailableError("failed to seek PDF reader before upload", err)
	}
	if err := f.storage.Upload(ctx, storageKey, reader); err != nil {
		return nil, port.NewStorageError("failed to upload source file to temporary storage", err)
	}

	return &port.FetchResult{
		StorageKey: storageKey,
		FileSize:   limited.bytesRead,
		PageCount:  info.PageCount,
		IsTextPDF:  info.IsTextPDF,
	}, nil
}

// limitedReader wraps an io.Reader and tracks bytes read.
// When bytesRead exceeds the limit, it sets exceeded=true and returns io.EOF.
type limitedReader struct {
	r         io.Reader
	limit     int64
	bytesRead int64
	exceeded  bool
}

func (lr *limitedReader) Read(p []byte) (int, error) {
	if lr.exceeded {
		return 0, io.EOF
	}
	// Cap the read buffer so at most limit+1 bytes are ever read from the
	// underlying reader. This detects overflow with at most 1 byte of overshoot
	// rather than an entire buffer's worth.
	remaining := lr.limit - lr.bytesRead + 1
	if remaining <= 0 {
		lr.exceeded = true
		return 0, io.EOF
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := lr.r.Read(p)
	lr.bytesRead += int64(n)
	if lr.bytesRead > lr.limit {
		lr.exceeded = true
		return n, io.EOF
	}
	return n, err
}

// classifyDownloadError converts download errors into appropriate domain errors.
// Context errors are passed through raw. DomainErrors are passed through unchanged.
// Unknown errors become SERVICE_UNAVAILABLE.
func classifyDownloadError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if port.IsDomainError(err) {
		return err
	}
	return port.NewServiceUnavailableError("file download failed", err)
}

// compile-time check: Fetcher implements SourceFileFetcherPort.
var _ port.SourceFileFetcherPort = (*Fetcher)(nil)
