package objectstorage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/port"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// Compile-time interface check.
var _ port.ObjectStoragePort = (*Client)(nil)

// S3API abstracts the subset of the S3 client used by this adapter.
// This enables dependency inversion for unit testing.
type S3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
}

// PresignAPI abstracts presigned URL generation. The AWS SDK uses a separate
// presign client, so this is a separate interface.
type PresignAPI interface {
	PresignGetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

// Client implements port.ObjectStoragePort using an S3-compatible backend.
type Client struct {
	s3         S3API
	presigner  PresignAPI
	bucket     string
	defaultTTL time.Duration
}

// NewClient creates a new Object Storage client configured for Yandex Object Storage
// (S3-compatible) with static credentials, path-style addressing, and retry.
func NewClient(cfg config.StorageConfig) *Client {
	s3Client := s3.New(s3.Options{
		Region: cfg.Region,
		EndpointResolver: s3.EndpointResolverFunc(func(region string, options s3.EndpointResolverOptions) (aws.Endpoint, error) {
			return aws.Endpoint{
				URL:               cfg.Endpoint,
				HostnameImmutable: true,
			}, nil
		}),
		Credentials:      credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		UsePathStyle:     true,
		RetryMaxAttempts: 3,
	})

	presigner := s3.NewPresignClient(s3Client)

	return &Client{
		s3:         s3Client,
		presigner:  presigner,
		bucket:     cfg.Bucket,
		defaultTTL: cfg.PresignedURLTTL,
	}
}

// newClientWithS3 is a test constructor that injects mock implementations.
func newClientWithS3(api S3API, presigner PresignAPI, bucket string, defaultTTL time.Duration) *Client {
	return &Client{
		s3:         api,
		presigner:  presigner,
		bucket:     bucket,
		defaultTTL: defaultTTL,
	}
}

// PutObject uploads an object to the specified key.
func (c *Client) PutObject(ctx context.Context, key string, data io.Reader, contentType string) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        data,
		ContentType: aws.String(contentType),
	})
	return mapError(err, "PutObject")
}

// GetObject retrieves an object by key. The caller must close the returned ReadCloser.
func (c *Client) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	output, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, mapError(err, "GetObject")
	}
	if output.Body == nil {
		return nil, port.NewStorageError("GetObject: response body is nil", nil)
	}
	return output.Body, nil
}

// DeleteObject removes a single object by key. S3 returns success for
// non-existent keys, making this inherently idempotent.
func (c *Client) DeleteObject(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	return mapError(err, "DeleteObject")
}

// HeadObject checks if an object exists and returns its size.
// Returns exists=false if the object does not exist (not an error).
func (c *Client) HeadObject(ctx context.Context, key string) (int64, bool, error) {
	output, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFoundError(err) {
			return 0, false, nil
		}
		return 0, false, mapError(err, "HeadObject")
	}
	return output.ContentLength, true, nil
}

// GeneratePresignedURL generates a time-limited URL for direct client download.
// If expiry is zero, the configured default TTL is used.
func (c *Client) GeneratePresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	if expiry < 0 {
		return "", port.NewStorageError("GeneratePresignedURL: expiry must not be negative", nil)
	}
	if expiry == 0 {
		expiry = c.defaultTTL
	}

	req, err := c.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expiry
	})
	if err != nil {
		return "", mapError(err, "GeneratePresignedURL")
	}
	return req.URL, nil
}

// DeleteByPrefix removes all objects with the given key prefix (batch cleanup).
// Processes objects in pages of up to 1000 (S3 maximum per DeleteObjects call).
func (c *Client) DeleteByPrefix(ctx context.Context, prefix string) error {
	if prefix == "" {
		return port.NewStorageError("DeleteByPrefix: empty prefix is not allowed", nil)
	}

	var continuationToken *string
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		listOutput, err := c.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.bucket),
			Prefix:            aws.String(prefix),
			MaxKeys:           1000,
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return mapError(err, "DeleteByPrefix/ListObjectsV2")
		}

		if len(listOutput.Contents) == 0 {
			return nil
		}

		objects := make([]types.ObjectIdentifier, len(listOutput.Contents))
		for i, obj := range listOutput.Contents {
			objects[i] = types.ObjectIdentifier{Key: obj.Key}
		}

		deleteOutput, err := c.s3.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(c.bucket),
			Delete: &types.Delete{
				Objects: objects,
				Quiet:   true,
			},
		})
		if err != nil {
			return mapError(err, "DeleteByPrefix/DeleteObjects")
		}

		if deleteOutput != nil && len(deleteOutput.Errors) > 0 {
			firstErr := deleteOutput.Errors[0]
			return port.NewStorageError(
				fmt.Sprintf("DeleteByPrefix: %d of %d objects failed to delete; first: %s (key: %s)",
					len(deleteOutput.Errors), len(objects),
					aws.ToString(firstErr.Message), aws.ToString(firstErr.Key)),
				nil,
			)
		}

		if !listOutput.IsTruncated {
			return nil
		}
		continuationToken = listOutput.NextContinuationToken
	}
}

// isNotFoundError returns true if the error indicates the object does not exist.
// S3 HeadObject returns either types.NotFound or a smithy.APIError with code
// "NotFound" or "NoSuchKey" depending on the S3-compatible implementation.
func isNotFoundError(err error) bool {
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return true
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		return code == "NotFound" || code == "NoSuchKey"
	}

	return false
}
