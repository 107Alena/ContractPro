package objectstorage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"contractpro/document-management/internal/domain/port"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockS3 struct {
	putObjectFn      func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	getObjectFn      func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	deleteObjectFn   func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	headObjectFn     func(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	listObjectsV2Fn  func(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	deleteObjectsFn  func(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
}

var _ S3API = (*mockS3)(nil)

func (m *mockS3) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return m.putObjectFn(ctx, params, optFns...)
}

func (m *mockS3) GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return m.getObjectFn(ctx, params, optFns...)
}

func (m *mockS3) DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	return m.deleteObjectFn(ctx, params, optFns...)
}

func (m *mockS3) HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return m.headObjectFn(ctx, params, optFns...)
}

func (m *mockS3) ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return m.listObjectsV2Fn(ctx, params, optFns...)
}

func (m *mockS3) DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	return m.deleteObjectsFn(ctx, params, optFns...)
}

type mockPresigner struct {
	presignGetObjectFn func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

var _ PresignAPI = (*mockPresigner)(nil)

func (m *mockPresigner) PresignGetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	return m.presignGetObjectFn(ctx, params, optFns...)
}

// apiError implements smithy.APIError for testing error classification.
type apiError struct {
	code    string
	message string
}

func (e *apiError) Error() string                   { return e.code + ": " + e.message }
func (e *apiError) ErrorCode() string                { return e.code }
func (e *apiError) ErrorMessage() string             { return e.message }
func (e *apiError) ErrorFault() smithy.ErrorFault    { return smithy.FaultUnknown }

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

const testBucket = "test-bucket"

func newTestClient(s3api S3API, presigner PresignAPI) *Client {
	return newClientWithS3(s3api, presigner, testBucket, 5*time.Minute)
}

// ---------------------------------------------------------------------------
// PutObject tests
// ---------------------------------------------------------------------------

func TestPutObject_Success(t *testing.T) {
	var capturedInput *s3.PutObjectInput
	mock := &mockS3{
		putObjectFn: func(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			capturedInput = params
			return &s3.PutObjectOutput{}, nil
		},
	}
	c := newTestClient(mock, nil)

	body := bytes.NewReader([]byte("hello"))
	err := c.PutObject(context.Background(), "org/doc/ver/SEMANTIC_TREE", body, "application/json")

	require.NoError(t, err)
	assert.Equal(t, testBucket, aws.ToString(capturedInput.Bucket))
	assert.Equal(t, "org/doc/ver/SEMANTIC_TREE", aws.ToString(capturedInput.Key))
	assert.Equal(t, "application/json", aws.ToString(capturedInput.ContentType))
}

func TestPutObject_S3Error(t *testing.T) {
	mock := &mockS3{
		putObjectFn: func(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			return nil, &apiError{code: "InternalError", message: "server error"}
		},
	}
	c := newTestClient(mock, nil)

	err := c.PutObject(context.Background(), "key", bytes.NewReader(nil), "application/json")

	require.Error(t, err)
	assert.True(t, port.IsRetryable(err))
	assert.Equal(t, port.ErrCodeStorageFailed, port.ErrorCode(err))
}

func TestPutObject_ContextCancelled(t *testing.T) {
	mock := &mockS3{
		putObjectFn: func(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			return nil, context.Canceled
		},
	}
	c := newTestClient(mock, nil)

	err := c.PutObject(context.Background(), "key", bytes.NewReader(nil), "application/json")

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestPutObject_AccessDenied(t *testing.T) {
	mock := &mockS3{
		putObjectFn: func(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			return nil, &apiError{code: "AccessDenied", message: "access denied"}
		},
	}
	c := newTestClient(mock, nil)

	err := c.PutObject(context.Background(), "key", bytes.NewReader(nil), "application/json")

	require.Error(t, err)
	assert.False(t, port.IsRetryable(err))
	assert.Equal(t, port.ErrCodeStorageFailed, port.ErrorCode(err))
}

// ---------------------------------------------------------------------------
// GetObject tests
// ---------------------------------------------------------------------------

func TestGetObject_Success(t *testing.T) {
	expectedContent := []byte("file content")
	mock := &mockS3{
		getObjectFn: func(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			assert.Equal(t, testBucket, aws.ToString(params.Bucket))
			assert.Equal(t, "test-key", aws.ToString(params.Key))
			return &s3.GetObjectOutput{
				Body: io.NopCloser(bytes.NewReader(expectedContent)),
			}, nil
		},
	}
	c := newTestClient(mock, nil)

	body, err := c.GetObject(context.Background(), "test-key")

	require.NoError(t, err)
	defer body.Close()
	data, _ := io.ReadAll(body)
	assert.Equal(t, expectedContent, data)
}

func TestGetObject_NoSuchKey(t *testing.T) {
	mock := &mockS3{
		getObjectFn: func(_ context.Context, _ *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			return nil, &apiError{code: "NoSuchKey", message: "not found"}
		},
	}
	c := newTestClient(mock, nil)

	body, err := c.GetObject(context.Background(), "missing-key")

	assert.Nil(t, body)
	require.Error(t, err)
	assert.False(t, port.IsRetryable(err))
}

func TestGetObject_NilBody(t *testing.T) {
	mock := &mockS3{
		getObjectFn: func(_ context.Context, _ *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			return &s3.GetObjectOutput{Body: nil}, nil
		},
	}
	c := newTestClient(mock, nil)

	body, err := c.GetObject(context.Background(), "key")

	assert.Nil(t, body)
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeStorageFailed, port.ErrorCode(err))
}

func TestGetObject_ContextCancelled(t *testing.T) {
	mock := &mockS3{
		getObjectFn: func(_ context.Context, _ *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			return nil, context.Canceled
		},
	}
	c := newTestClient(mock, nil)

	_, err := c.GetObject(context.Background(), "key")

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

// ---------------------------------------------------------------------------
// DeleteObject tests
// ---------------------------------------------------------------------------

func TestDeleteObject_Success(t *testing.T) {
	mock := &mockS3{
		deleteObjectFn: func(_ context.Context, params *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			assert.Equal(t, testBucket, aws.ToString(params.Bucket))
			assert.Equal(t, "test-key", aws.ToString(params.Key))
			return &s3.DeleteObjectOutput{}, nil
		},
	}
	c := newTestClient(mock, nil)

	err := c.DeleteObject(context.Background(), "test-key")

	require.NoError(t, err)
}

func TestDeleteObject_Idempotent(t *testing.T) {
	// S3 returns success for non-existent keys.
	mock := &mockS3{
		deleteObjectFn: func(_ context.Context, _ *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			return &s3.DeleteObjectOutput{}, nil
		},
	}
	c := newTestClient(mock, nil)

	err := c.DeleteObject(context.Background(), "non-existent-key")

	require.NoError(t, err)
}

func TestDeleteObject_S3Error(t *testing.T) {
	mock := &mockS3{
		deleteObjectFn: func(_ context.Context, _ *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			return nil, &apiError{code: "InternalError", message: "server error"}
		},
	}
	c := newTestClient(mock, nil)

	err := c.DeleteObject(context.Background(), "key")

	require.Error(t, err)
	assert.True(t, port.IsRetryable(err))
}

// ---------------------------------------------------------------------------
// HeadObject tests
// ---------------------------------------------------------------------------

func TestHeadObject_Exists(t *testing.T) {
	mock := &mockS3{
		headObjectFn: func(_ context.Context, params *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			assert.Equal(t, testBucket, aws.ToString(params.Bucket))
			return &s3.HeadObjectOutput{ContentLength: 12345}, nil
		},
	}
	c := newTestClient(mock, nil)

	size, exists, err := c.HeadObject(context.Background(), "test-key")

	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, int64(12345), size)
}

func TestHeadObject_NotFound(t *testing.T) {
	mock := &mockS3{
		headObjectFn: func(_ context.Context, _ *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			return nil, &types.NotFound{Message: aws.String("not found")}
		},
	}
	c := newTestClient(mock, nil)

	size, exists, err := c.HeadObject(context.Background(), "missing-key")

	require.NoError(t, err)
	assert.False(t, exists)
	assert.Equal(t, int64(0), size)
}

func TestHeadObject_NoSuchKey(t *testing.T) {
	mock := &mockS3{
		headObjectFn: func(_ context.Context, _ *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			return nil, &apiError{code: "NoSuchKey", message: "not found"}
		},
	}
	c := newTestClient(mock, nil)

	size, exists, err := c.HeadObject(context.Background(), "missing-key")

	require.NoError(t, err)
	assert.False(t, exists)
	assert.Equal(t, int64(0), size)
}

func TestHeadObject_S3Error(t *testing.T) {
	mock := &mockS3{
		headObjectFn: func(_ context.Context, _ *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			return nil, &apiError{code: "InternalError", message: "server error"}
		},
	}
	c := newTestClient(mock, nil)

	_, exists, err := c.HeadObject(context.Background(), "key")

	require.Error(t, err)
	assert.False(t, exists)
	assert.True(t, port.IsRetryable(err))
}

func TestHeadObject_ContextCancelled(t *testing.T) {
	mock := &mockS3{
		headObjectFn: func(_ context.Context, _ *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			return nil, context.Canceled
		},
	}
	c := newTestClient(mock, nil)

	_, _, err := c.HeadObject(context.Background(), "key")

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

// ---------------------------------------------------------------------------
// GeneratePresignedURL tests
// ---------------------------------------------------------------------------

func TestGeneratePresignedURL_Success(t *testing.T) {
	presigner := &mockPresigner{
		presignGetObjectFn: func(_ context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
			assert.Equal(t, testBucket, aws.ToString(params.Bucket))
			assert.Equal(t, "test-key", aws.ToString(params.Key))
			// Verify expiry was applied.
			opts := s3.PresignOptions{}
			for _, fn := range optFns {
				fn(&opts)
			}
			assert.Equal(t, 15*time.Minute, opts.Expires)
			return &v4.PresignedHTTPRequest{URL: "https://storage.example.com/test-key?signed=1"}, nil
		},
	}
	c := newTestClient(nil, presigner)

	url, err := c.GeneratePresignedURL(context.Background(), "test-key", 15*time.Minute)

	require.NoError(t, err)
	assert.Equal(t, "https://storage.example.com/test-key?signed=1", url)
}

func TestGeneratePresignedURL_ZeroExpiry_UsesDefault(t *testing.T) {
	presigner := &mockPresigner{
		presignGetObjectFn: func(_ context.Context, _ *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
			opts := s3.PresignOptions{}
			for _, fn := range optFns {
				fn(&opts)
			}
			assert.Equal(t, 5*time.Minute, opts.Expires, "should use default TTL when expiry is zero")
			return &v4.PresignedHTTPRequest{URL: "https://example.com/signed"}, nil
		},
	}
	c := newTestClient(nil, presigner)

	url, err := c.GeneratePresignedURL(context.Background(), "key", 0)

	require.NoError(t, err)
	assert.NotEmpty(t, url)
}

func TestGeneratePresignedURL_CustomExpiry(t *testing.T) {
	presigner := &mockPresigner{
		presignGetObjectFn: func(_ context.Context, _ *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
			opts := s3.PresignOptions{}
			for _, fn := range optFns {
				fn(&opts)
			}
			assert.Equal(t, 30*time.Minute, opts.Expires)
			return &v4.PresignedHTTPRequest{URL: "https://example.com/signed"}, nil
		},
	}
	c := newTestClient(nil, presigner)

	_, err := c.GeneratePresignedURL(context.Background(), "key", 30*time.Minute)

	require.NoError(t, err)
}

func TestGeneratePresignedURL_NegativeExpiry(t *testing.T) {
	c := newTestClient(nil, nil)

	url, err := c.GeneratePresignedURL(context.Background(), "key", -1*time.Minute)

	assert.Empty(t, url)
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeStorageFailed, port.ErrorCode(err))
}

func TestGeneratePresignedURL_ContextCancelled(t *testing.T) {
	presigner := &mockPresigner{
		presignGetObjectFn: func(_ context.Context, _ *s3.GetObjectInput, _ ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
			return nil, context.Canceled
		},
	}
	c := newTestClient(nil, presigner)

	_, err := c.GeneratePresignedURL(context.Background(), "key", 5*time.Minute)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestGeneratePresignedURL_Error(t *testing.T) {
	presigner := &mockPresigner{
		presignGetObjectFn: func(_ context.Context, _ *s3.GetObjectInput, _ ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
			return nil, &apiError{code: "InternalError", message: "presign failed"}
		},
	}
	c := newTestClient(nil, presigner)

	url, err := c.GeneratePresignedURL(context.Background(), "key", 5*time.Minute)

	assert.Empty(t, url)
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeStorageFailed, port.ErrorCode(err))
}

// ---------------------------------------------------------------------------
// DeleteByPrefix tests
// ---------------------------------------------------------------------------

func TestDeleteByPrefix_ZeroObjects(t *testing.T) {
	mock := &mockS3{
		listObjectsV2Fn: func(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			return &s3.ListObjectsV2Output{Contents: []types.Object{}}, nil
		},
	}
	c := newTestClient(mock, nil)

	err := c.DeleteByPrefix(context.Background(), "org/doc/ver/")

	require.NoError(t, err)
}

func TestDeleteByPrefix_SinglePage(t *testing.T) {
	var deletedKeys []string
	mock := &mockS3{
		listObjectsV2Fn: func(_ context.Context, params *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			assert.Equal(t, "org/doc/ver/", aws.ToString(params.Prefix))
			return &s3.ListObjectsV2Output{
				Contents: []types.Object{
					{Key: aws.String("org/doc/ver/SEMANTIC_TREE")},
					{Key: aws.String("org/doc/ver/OCR_RAW")},
				},
				IsTruncated: false,
			}, nil
		},
		deleteObjectsFn: func(_ context.Context, params *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
			for _, obj := range params.Delete.Objects {
				deletedKeys = append(deletedKeys, aws.ToString(obj.Key))
			}
			return &s3.DeleteObjectsOutput{}, nil
		},
	}
	c := newTestClient(mock, nil)

	err := c.DeleteByPrefix(context.Background(), "org/doc/ver/")

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"org/doc/ver/SEMANTIC_TREE", "org/doc/ver/OCR_RAW"}, deletedKeys)
}

func TestDeleteByPrefix_MultiplePages(t *testing.T) {
	callCount := 0
	mock := &mockS3{
		listObjectsV2Fn: func(_ context.Context, params *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			callCount++
			if callCount == 1 {
				return &s3.ListObjectsV2Output{
					Contents:              []types.Object{{Key: aws.String("key1")}},
					IsTruncated:           true,
					NextContinuationToken: aws.String("token-2"),
				}, nil
			}
			assert.Equal(t, "token-2", aws.ToString(params.ContinuationToken))
			return &s3.ListObjectsV2Output{
				Contents:    []types.Object{{Key: aws.String("key2")}},
				IsTruncated: false,
			}, nil
		},
		deleteObjectsFn: func(_ context.Context, _ *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
			return &s3.DeleteObjectsOutput{}, nil
		},
	}
	c := newTestClient(mock, nil)

	err := c.DeleteByPrefix(context.Background(), "prefix/")

	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestDeleteByPrefix_EmptyPrefix(t *testing.T) {
	c := newTestClient(&mockS3{}, nil)

	err := c.DeleteByPrefix(context.Background(), "")

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeStorageFailed, port.ErrorCode(err))
}

func TestDeleteByPrefix_ListError(t *testing.T) {
	mock := &mockS3{
		listObjectsV2Fn: func(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			return nil, &apiError{code: "InternalError", message: "list failed"}
		},
	}
	c := newTestClient(mock, nil)

	err := c.DeleteByPrefix(context.Background(), "prefix/")

	require.Error(t, err)
	assert.True(t, port.IsRetryable(err))
}

func TestDeleteByPrefix_DeleteObjectsError(t *testing.T) {
	mock := &mockS3{
		listObjectsV2Fn: func(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			return &s3.ListObjectsV2Output{
				Contents: []types.Object{{Key: aws.String("key1")}},
			}, nil
		},
		deleteObjectsFn: func(_ context.Context, _ *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
			return nil, &apiError{code: "InternalError", message: "delete batch failed"}
		},
	}
	c := newTestClient(mock, nil)

	err := c.DeleteByPrefix(context.Background(), "prefix/")

	require.Error(t, err)
	assert.True(t, port.IsRetryable(err))
}

func TestDeleteByPrefix_PartialDeleteError(t *testing.T) {
	mock := &mockS3{
		listObjectsV2Fn: func(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			return &s3.ListObjectsV2Output{
				Contents: []types.Object{{Key: aws.String("key1")}, {Key: aws.String("key2")}},
			}, nil
		},
		deleteObjectsFn: func(_ context.Context, _ *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
			return &s3.DeleteObjectsOutput{
				Errors: []types.Error{
					{Key: aws.String("key2"), Message: aws.String("access denied")},
				},
			}, nil
		},
	}
	c := newTestClient(mock, nil)

	err := c.DeleteByPrefix(context.Background(), "prefix/")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 of 2 objects failed to delete")
	assert.Contains(t, err.Error(), "key2")
}

func TestDeleteByPrefix_ContextCancelledBetweenPages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0
	mock := &mockS3{
		listObjectsV2Fn: func(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			callCount++
			if callCount == 1 {
				cancel() // Cancel after first page.
				return &s3.ListObjectsV2Output{
					Contents:              []types.Object{{Key: aws.String("key1")}},
					IsTruncated:           true,
					NextContinuationToken: aws.String("token"),
				}, nil
			}
			t.Fatal("should not reach second page")
			return nil, nil
		},
		deleteObjectsFn: func(_ context.Context, _ *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
			return &s3.DeleteObjectsOutput{}, nil
		},
	}
	c := newTestClient(mock, nil)

	err := c.DeleteByPrefix(ctx, "prefix/")

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

// ---------------------------------------------------------------------------
// Error mapping tests
// ---------------------------------------------------------------------------

func TestMapError_Nil(t *testing.T) {
	assert.NoError(t, mapError(nil, "op"))
}

func TestMapError_ContextCanceled(t *testing.T) {
	err := mapError(context.Canceled, "op")
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestMapError_ContextDeadlineExceeded(t *testing.T) {
	err := mapError(context.DeadlineExceeded, "op")
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestMapError_RetryableAPIError(t *testing.T) {
	err := mapError(&apiError{code: "InternalError", message: "boom"}, "op")
	require.Error(t, err)
	assert.True(t, port.IsRetryable(err))
	assert.Equal(t, port.ErrCodeStorageFailed, port.ErrorCode(err))
}

func TestMapError_NonRetryableAPIError_AccessDenied(t *testing.T) {
	err := mapError(&apiError{code: "AccessDenied", message: "forbidden"}, "op")
	require.Error(t, err)
	assert.False(t, port.IsRetryable(err))
}

func TestMapError_NonRetryableAPIError_NoSuchBucket(t *testing.T) {
	err := mapError(&apiError{code: "NoSuchBucket", message: "bucket not found"}, "op")
	require.Error(t, err)
	assert.False(t, port.IsRetryable(err))
}

func TestMapError_UnknownError(t *testing.T) {
	err := mapError(errors.New("network timeout"), "op")
	require.Error(t, err)
	assert.True(t, port.IsRetryable(err))
	assert.Equal(t, port.ErrCodeStorageFailed, port.ErrorCode(err))
}

// ---------------------------------------------------------------------------
// isNotFoundError tests
// ---------------------------------------------------------------------------

func TestIsNotFoundError_TypesNotFound(t *testing.T) {
	err := &types.NotFound{Message: aws.String("not found")}
	assert.True(t, isNotFoundError(err))
}

func TestIsNotFoundError_APIErrorNotFound(t *testing.T) {
	assert.True(t, isNotFoundError(&apiError{code: "NotFound"}))
}

func TestIsNotFoundError_APIErrorNoSuchKey(t *testing.T) {
	assert.True(t, isNotFoundError(&apiError{code: "NoSuchKey"}))
}

func TestIsNotFoundError_OtherError(t *testing.T) {
	assert.False(t, isNotFoundError(errors.New("something else")))
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestInterfaceCompliance(t *testing.T) {
	var _ port.ObjectStoragePort = (*Client)(nil)
	var _ S3API = (*mockS3)(nil)
	var _ PresignAPI = (*mockPresigner)(nil)
}
