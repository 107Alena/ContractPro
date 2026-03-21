package fetcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// Fetcher implements SourceFileFetcherPort — downloads a PDF by URL,
// validates file size during streaming, and saves the file to temporary storage.
type Fetcher struct {
	downloader  port.SourceFileDownloaderPort
	storage     port.TempStoragePort
	maxFileSize int64
}

// NewFetcher creates a Fetcher with the given dependencies.
// maxFileSize is the maximum allowed file size in bytes (typically 20 MB).
func NewFetcher(downloader port.SourceFileDownloaderPort, storage port.TempStoragePort, maxFileSize int64) *Fetcher {
	return &Fetcher{
		downloader:  downloader,
		storage:     storage,
		maxFileSize: maxFileSize,
	}
}

// Fetch downloads the source file from cmd.FileURL, validates its size,
// and uploads it to temporary storage under key "{job_id}/source.pdf".
// Returns a FetchResult with the storage key and actual file size.
// PageCount and IsTextPDF are left at zero values (TASK-024 scope).
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

	storageKey := fmt.Sprintf("%s/source.pdf", cmd.JobID)

	limited := &limitedReader{
		r:     body,
		limit: f.maxFileSize,
	}

	uploadErr := f.storage.Upload(ctx, storageKey, limited)

	// If we detected size exceeded during streaming, clean up and return FILE_TOO_LARGE
	// regardless of whether upload succeeded or failed.
	if limited.exceeded {
		// Best-effort cleanup with a short timeout to avoid blocking indefinitely.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = f.storage.Delete(cleanupCtx, storageKey)
		return nil, port.NewFileTooLargeError(
			fmt.Sprintf("file size exceeds limit %d bytes during streaming", f.maxFileSize),
		)
	}

	if uploadErr != nil {
		return nil, port.NewStorageError("failed to upload source file to temporary storage", uploadErr)
	}

	return &port.FetchResult{
		StorageKey: storageKey,
		FileSize:   limited.bytesRead,
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
