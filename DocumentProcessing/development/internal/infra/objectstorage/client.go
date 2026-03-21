package objectstorage

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"contractpro/document-processing/internal/config"
	"contractpro/document-processing/internal/domain/port"
)

// S3API is a consumer-side interface covering the subset of s3.Client methods
// used by this adapter. Defining it here keeps the dependency inverted and
// enables unit testing with a mock.
type S3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
}

// Compile-time interface compliance check.
var _ port.TempStoragePort = (*Client)(nil)

// Client implements port.TempStoragePort on top of an S3-compatible object
// storage (Yandex Object Storage).
type Client struct {
	s3     S3API
	bucket string
}

// NewClient creates a Client configured for the given StorageConfig.
// It builds a real aws-sdk-go-v2 S3 client with static credentials,
// a custom endpoint, and path-style addressing (required by Yandex Object Storage).
func NewClient(cfg config.StorageConfig) *Client {
	s3Client := s3.New(s3.Options{
		Region:       cfg.Region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		UsePathStyle: true,
	})

	return &Client{
		s3:     s3Client,
		bucket: cfg.Bucket,
	}
}

// newClientWithS3 creates a Client with an injected S3API (for testing).
func newClientWithS3(api S3API, bucket string) *Client {
	return &Client{
		s3:     api,
		bucket: bucket,
	}
}

// Upload stores data under the given key in the configured bucket.
func (c *Client) Upload(ctx context.Context, key string, data io.Reader) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   data,
	})
	if err != nil {
		return mapError(err, "Upload")
	}
	return nil
}

// Download retrieves the object at key and returns an io.ReadCloser
// over the response body. The caller is responsible for closing it.
func (c *Client) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, mapError(err, "Download")
	}
	if out.Body == nil {
		return nil, port.NewStorageError(
			fmt.Sprintf("objectstorage: Download: nil body for key %q", key), nil,
		)
	}
	return out.Body, nil
}

// Delete removes the object at key. S3 returns success even when the key
// does not exist, so this operation is idempotent.
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return mapError(err, "Delete")
	}
	return nil
}

// DeleteByPrefix lists all objects whose key starts with prefix and removes
// them via batch delete. Handles pagination (up to 1000 keys per page) and
// checks ctx.Err() between pages.
// If no objects match the prefix, the function returns nil without error.
func (c *Client) DeleteByPrefix(ctx context.Context, prefix string) error {
	if prefix == "" {
		return port.NewStorageError("objectstorage: DeleteByPrefix: empty prefix", nil)
	}

	var continuationToken *string

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		listOut, err := c.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return mapError(err, "DeleteByPrefix/List")
		}

		if len(listOut.Contents) == 0 {
			return nil
		}

		objects := make([]types.ObjectIdentifier, len(listOut.Contents))
		for i, obj := range listOut.Contents {
			objects[i] = types.ObjectIdentifier{Key: obj.Key}
		}

		delOut, err := c.s3.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(c.bucket),
			Delete: &types.Delete{
				Objects: objects,
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			return mapError(err, "DeleteByPrefix/Delete")
		}
		if len(delOut.Errors) > 0 {
			return port.NewStorageError(
				fmt.Sprintf("objectstorage: DeleteByPrefix/Delete: %d objects failed, first: %s: %s",
					len(delOut.Errors),
					aws.ToString(delOut.Errors[0].Key),
					aws.ToString(delOut.Errors[0].Message)),
				fmt.Errorf("partial delete failure"),
			)
		}

		if !aws.ToBool(listOut.IsTruncated) {
			return nil
		}
		continuationToken = listOut.NextContinuationToken
	}
}
