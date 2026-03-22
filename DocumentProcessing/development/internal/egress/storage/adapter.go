package storage

import (
	"context"
	"io"

	"contractpro/document-processing/internal/domain/port"
)

// StorageClient abstracts the object storage client. Implemented by objectstorage.Client.
// Implementations must be safe for concurrent use by multiple goroutines.
type StorageClient interface {
	Upload(ctx context.Context, key string, data io.Reader) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	DeleteByPrefix(ctx context.Context, prefix string) error
}

// Compile-time interface compliance check.
var _ port.TempStoragePort = (*Adapter)(nil)

// Adapter implements port.TempStoragePort by delegating to a StorageClient
// with an optional global key prefix prepended to all keys.
type Adapter struct {
	client    StorageClient
	keyPrefix string // global prefix prepended to all keys
}

// NewAdapter creates an Adapter with the given storage client and key prefix.
// Panics if client is nil (programmer error at startup).
func NewAdapter(client StorageClient, keyPrefix string) *Adapter {
	if client == nil {
		panic("storage: client must not be nil")
	}
	return &Adapter{
		client:    client,
		keyPrefix: keyPrefix,
	}
}

// fullKey returns keyPrefix + key.
func (a *Adapter) fullKey(key string) string {
	return a.keyPrefix + key
}

// Upload stores data under the given key. Returns a non-retryable DomainError
// if key is empty. Client errors pass through unchanged.
func (a *Adapter) Upload(ctx context.Context, key string, data io.Reader) error {
	if key == "" {
		return &port.DomainError{
			Code:      port.ErrCodeStorageFailed,
			Message:   "storage: Upload: empty key",
			Retryable: false,
		}
	}
	return a.client.Upload(ctx, a.fullKey(key), data)
}

// Download retrieves the object at key and returns an io.ReadCloser.
// The caller is responsible for closing it. Returns a non-retryable DomainError
// if key is empty. Client errors pass through unchanged.
func (a *Adapter) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	if key == "" {
		return nil, &port.DomainError{
			Code:      port.ErrCodeStorageFailed,
			Message:   "storage: Download: empty key",
			Retryable: false,
		}
	}
	return a.client.Download(ctx, a.fullKey(key))
}

// Delete removes the object at key. Returns a non-retryable DomainError
// if key is empty. Client errors pass through unchanged.
func (a *Adapter) Delete(ctx context.Context, key string) error {
	if key == "" {
		return &port.DomainError{
			Code:      port.ErrCodeStorageFailed,
			Message:   "storage: Delete: empty key",
			Retryable: false,
		}
	}
	return a.client.Delete(ctx, a.fullKey(key))
}

// DeleteByPrefix removes all objects whose key starts with prefix.
// Returns a non-retryable DomainError if prefix is empty (safety: empty prefix
// would delete everything). Client errors pass through unchanged.
func (a *Adapter) DeleteByPrefix(ctx context.Context, prefix string) error {
	if prefix == "" {
		return &port.DomainError{
			Code:      port.ErrCodeStorageFailed,
			Message:   "storage: DeleteByPrefix: empty prefix",
			Retryable: false,
		}
	}
	return a.client.DeleteByPrefix(ctx, a.fullKey(prefix))
}
