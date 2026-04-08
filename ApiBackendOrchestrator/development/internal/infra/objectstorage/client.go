package objectstorage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/sony/gobreaker/v2"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

// S3API is a consumer-side interface covering the S3 operations used by
// the orchestrator. Defined here to invert the AWS SDK dependency.
type S3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
}

const (
	maxRetries     = 2
	baseDelay      = 500 * time.Millisecond
	jitterFraction = 0.25
	defaultOpTimeout = 15 * time.Second
)

// Client is an S3-compatible Object Storage client with retry and circuit
// breaker. It provides upload, delete, and existence-check operations.
type Client struct {
	s3            S3API
	bucket        string
	cb            *gobreaker.CircuitBreaker[struct{}]
	uploadTimeout time.Duration
	opTimeout     time.Duration
	maxRetries    int
	log           *logger.Logger
}

// NewClient creates a Client with a real aws-sdk-go-v2 S3 backend.
// No connectivity check is performed on startup (S3 has no lightweight ping).
func NewClient(cfg config.StorageConfig, cbCfg config.CircuitBreakerConfig, log *logger.Logger) *Client {
	s3Client := s3.New(s3.Options{
		Region:       cfg.Region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		UsePathStyle: true,
	})
	return newClientWithS3(s3Client, cfg.Bucket, cbCfg, cfg.UploadTimeout, defaultOpTimeout, log)
}

// newClientWithS3 creates a Client with an injected S3API (for testing).
func newClientWithS3(
	api S3API,
	bucket string,
	cbCfg config.CircuitBreakerConfig,
	uploadTimeout time.Duration,
	opTimeout time.Duration,
	log *logger.Logger,
) *Client {
	componentLog := log.With("component", "object-storage")

	cb := gobreaker.NewCircuitBreaker[struct{}](gobreaker.Settings{
		Name:        "object-storage-client",
		MaxRequests: uint32(cbCfg.MaxRequests),
		Timeout:     cbCfg.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= uint32(cbCfg.FailureThreshold)
		},
		IsSuccessful: func(err error) bool {
			if err == nil {
				return true
			}
			return !isCBFailure(err)
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			componentLog.Warn(context.Background(),
				"circuit breaker state change",
				"name", name,
				"from", from.String(),
				"to", to.String(),
			)
		},
	})

	return &Client{
		s3:            api,
		bucket:        bucket,
		cb:            cb,
		uploadTimeout: uploadTimeout,
		opTimeout:     opTimeout,
		maxRetries:    maxRetries,
		log:           componentLog,
	}
}

// PutObject uploads data to the given key in the configured bucket.
//
// The caller MUST pass an io.ReadSeeker because the retry logic needs to
// Seek(0, io.SeekStart) before each attempt. Typical callers: *os.File,
// *bytes.Reader, multipart.File — all implement io.ReadSeeker.
func (c *Client) PutObject(ctx context.Context, key string, data io.ReadSeeker, contentType string) error {
	return c.executeWithRetry(ctx, "PutObject", c.uploadTimeout, func(attemptCtx context.Context) error {
		if _, err := data.Seek(0, io.SeekStart); err != nil {
			return &StorageError{
				Operation: "PutObject",
				Message:   "seek failed",
				Retryable: false,
				Cause:     err,
			}
		}
		_, err := c.s3.PutObject(attemptCtx, &s3.PutObjectInput{
			Bucket:      aws.String(c.bucket),
			Key:         aws.String(key),
			Body:        data,
			ContentType: aws.String(contentType),
		})
		if err != nil {
			return mapError(err, "PutObject")
		}
		return nil
	})
}

// DeleteObject removes the object at key. S3 returns success for
// non-existent keys, so this operation is idempotent.
func (c *Client) DeleteObject(ctx context.Context, key string) error {
	return c.executeWithRetry(ctx, "DeleteObject", c.opTimeout, func(attemptCtx context.Context) error {
		_, err := c.s3.DeleteObject(attemptCtx, &s3.DeleteObjectInput{
			Bucket: aws.String(c.bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return mapError(err, "DeleteObject")
		}
		return nil
	})
}

// HeadObject checks whether the object exists at key.
// Returns nil if the object exists, ErrObjectNotFound if it does not,
// or a StorageError for infrastructure failures.
func (c *Client) HeadObject(ctx context.Context, key string) error {
	return c.executeWithRetry(ctx, "HeadObject", c.opTimeout, func(attemptCtx context.Context) error {
		_, err := c.s3.HeadObject(attemptCtx, &s3.HeadObjectInput{
			Bucket: aws.String(c.bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return mapError(err, "HeadObject")
		}
		return nil
	})
}

// executeWithRetry runs fn up to maxRetries+1 times with exponential backoff.
// Each attempt is guarded by the circuit breaker and a per-attempt timeout.
func (c *Client) executeWithRetry(
	ctx context.Context,
	operation string,
	timeout time.Duration,
	fn func(attemptCtx context.Context) error,
) error {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if attempt > 0 {
			delay := c.backoffDelay(attempt)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		err := c.executeWithCB(attemptCtx, fn)
		cancel()

		if err == nil {
			return nil
		}
		lastErr = err

		if errors.Is(err, ErrCircuitOpen) {
			return err
		}
		if !isRetryable(err) {
			return err
		}

		if attempt < c.maxRetries {
			c.log.Warn(ctx, "retrying S3 operation",
				"operation", operation,
				"attempt", attempt+1,
				"max_attempts", c.maxRetries+1,
				logger.ErrorAttr(err),
			)
		}
	}
	return lastErr
}

// executeWithCB wraps a function call with the gobreaker circuit breaker.
func (c *Client) executeWithCB(ctx context.Context, fn func(ctx context.Context) error) error {
	_, err := c.cb.Execute(func() (struct{}, error) {
		return struct{}{}, fn(ctx)
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return fmt.Errorf("objectstorage: %w", ErrCircuitOpen)
		}
		return err
	}
	return nil
}

// backoffDelay computes the delay before retry attempt n (1-indexed).
// Attempt 1: 500ms + jitter, Attempt 2: 1000ms + jitter.
func (c *Client) backoffDelay(attempt int) time.Duration {
	delay := baseDelay * (1 << (attempt - 1))
	jitter := time.Duration(rand.Int64N(int64(float64(delay) * jitterFraction)))
	return delay + jitter
}
