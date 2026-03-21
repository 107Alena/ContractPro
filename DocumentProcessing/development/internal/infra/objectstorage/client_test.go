package objectstorage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"contractpro/document-processing/internal/domain/port"
)

// --- Mock ---

type mockS3 struct {
	putObjectFn     func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	getObjectFn     func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	deleteObjectFn  func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	listObjectsV2Fn func(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	deleteObjectsFn func(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
}

func (m *mockS3) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return m.putObjectFn(ctx, params, optFns...)
}

func (m *mockS3) GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return m.getObjectFn(ctx, params, optFns...)
}

func (m *mockS3) DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	return m.deleteObjectFn(ctx, params, optFns...)
}

func (m *mockS3) ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return m.listObjectsV2Fn(ctx, params, optFns...)
}

func (m *mockS3) DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	return m.deleteObjectsFn(ctx, params, optFns...)
}

// --- Helpers ---

// apiError implements smithy.APIError for testing.
type apiError struct {
	code    string
	message string
}

func (e *apiError) Error() string            { return fmt.Sprintf("%s: %s", e.code, e.message) }
func (e *apiError) ErrorCode() string         { return e.code }
func (e *apiError) ErrorMessage() string      { return e.message }
func (e *apiError) ErrorFault() smithy.ErrorFault { return smithy.FaultServer }

const testBucket = "test-bucket"

// --- Upload Tests ---

func TestUpload_Success(t *testing.T) {
	var capturedBucket, capturedKey string
	var capturedBody []byte

	mock := &mockS3{
		putObjectFn: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			capturedBucket = aws.ToString(params.Bucket)
			capturedKey = aws.ToString(params.Key)
			var err error
			capturedBody, err = io.ReadAll(params.Body)
			if err != nil {
				t.Fatalf("failed to read body: %v", err)
			}
			return &s3.PutObjectOutput{}, nil
		},
	}

	client := newClientWithS3(mock, testBucket)
	err := client.Upload(context.Background(), "jobs/123/doc.pdf", strings.NewReader("file-content"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedBucket != testBucket {
		t.Errorf("bucket = %q, want %q", capturedBucket, testBucket)
	}
	if capturedKey != "jobs/123/doc.pdf" {
		t.Errorf("key = %q, want %q", capturedKey, "jobs/123/doc.pdf")
	}
	if string(capturedBody) != "file-content" {
		t.Errorf("body = %q, want %q", string(capturedBody), "file-content")
	}
}

func TestUpload_S3Error(t *testing.T) {
	mock := &mockS3{
		putObjectFn: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			return nil, &apiError{code: "InternalError", message: "service unavailable"}
		},
	}

	client := newClientWithS3(mock, testBucket)
	err := client.Upload(context.Background(), "key", strings.NewReader("data"))

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("storage error should be retryable")
	}
}

func TestUpload_ContextCancelled(t *testing.T) {
	mock := &mockS3{
		putObjectFn: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			return nil, context.Canceled
		},
	}

	client := newClientWithS3(mock, testBucket)
	err := client.Upload(context.Background(), "key", strings.NewReader("data"))

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// --- Download Tests ---

func TestDownload_Success(t *testing.T) {
	want := "downloaded-data"
	mock := &mockS3{
		getObjectFn: func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			return &s3.GetObjectOutput{
				Body: io.NopCloser(strings.NewReader(want)),
			}, nil
		},
	}

	client := newClientWithS3(mock, testBucket)
	rc, err := client.Download(context.Background(), "jobs/123/doc.pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if string(got) != want {
		t.Errorf("body = %q, want %q", string(got), want)
	}
}

func TestDownload_NoSuchKey(t *testing.T) {
	mock := &mockS3{
		getObjectFn: func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			return nil, &types.NoSuchKey{Message: aws.String("key not found")}
		},
	}

	client := newClientWithS3(mock, testBucket)
	_, err := client.Download(context.Background(), "missing-key")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if port.IsRetryable(err) {
		t.Error("NoSuchKey should not be retryable")
	}
}

func TestDownload_AccessDenied(t *testing.T) {
	mock := &mockS3{
		getObjectFn: func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			return nil, &apiError{code: "AccessDenied", message: "access denied"}
		},
	}

	client := newClientWithS3(mock, testBucket)
	_, err := client.Download(context.Background(), "forbidden-key")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if !strings.Contains(err.Error(), "AccessDenied") {
		t.Errorf("error should contain 'AccessDenied', got %q", err.Error())
	}
	if port.IsRetryable(err) {
		t.Error("AccessDenied should not be retryable")
	}
}

func TestDownload_ContextCancelled(t *testing.T) {
	mock := &mockS3{
		getObjectFn: func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			return nil, context.Canceled
		},
	}

	client := newClientWithS3(mock, testBucket)
	_, err := client.Download(context.Background(), "key")

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// --- Delete Tests ---

func TestDelete_Success(t *testing.T) {
	mock := &mockS3{
		deleteObjectFn: func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			if aws.ToString(params.Key) != "jobs/123/doc.pdf" {
				t.Errorf("key = %q, want %q", aws.ToString(params.Key), "jobs/123/doc.pdf")
			}
			return &s3.DeleteObjectOutput{}, nil
		},
	}

	client := newClientWithS3(mock, testBucket)
	err := client.Delete(context.Background(), "jobs/123/doc.pdf")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDelete_Idempotent(t *testing.T) {
	// S3 returns success for non-existent keys — delete is idempotent.
	mock := &mockS3{
		deleteObjectFn: func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			return &s3.DeleteObjectOutput{}, nil
		},
	}

	client := newClientWithS3(mock, testBucket)
	err := client.Delete(context.Background(), "non-existent-key")

	if err != nil {
		t.Fatalf("expected nil error for non-existent key, got %v", err)
	}
}

func TestDelete_S3Error(t *testing.T) {
	mock := &mockS3{
		deleteObjectFn: func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			return nil, &apiError{code: "NoSuchBucket", message: "bucket not found"}
		},
	}

	client := newClientWithS3(mock, testBucket)
	err := client.Delete(context.Background(), "key")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
}

// --- DeleteByPrefix Tests ---

func TestDeleteByPrefix_ZeroObjects(t *testing.T) {
	mock := &mockS3{
		listObjectsV2Fn: func(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			return &s3.ListObjectsV2Output{
				Contents:    []types.Object{},
				IsTruncated: aws.Bool(false),
			}, nil
		},
	}

	client := newClientWithS3(mock, testBucket)
	err := client.DeleteByPrefix(context.Background(), "jobs/999/")

	if err != nil {
		t.Fatalf("expected nil for empty prefix, got %v", err)
	}
}

func TestDeleteByPrefix_SinglePage(t *testing.T) {
	var deletedKeys []string

	mock := &mockS3{
		listObjectsV2Fn: func(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			if aws.ToString(params.Prefix) != "jobs/123/" {
				t.Errorf("prefix = %q, want %q", aws.ToString(params.Prefix), "jobs/123/")
			}
			return &s3.ListObjectsV2Output{
				Contents: []types.Object{
					{Key: aws.String("jobs/123/a.pdf")},
					{Key: aws.String("jobs/123/b.pdf")},
				},
				IsTruncated: aws.Bool(false),
			}, nil
		},
		deleteObjectsFn: func(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
			for _, obj := range params.Delete.Objects {
				deletedKeys = append(deletedKeys, aws.ToString(obj.Key))
			}
			return &s3.DeleteObjectsOutput{}, nil
		},
	}

	client := newClientWithS3(mock, testBucket)
	err := client.DeleteByPrefix(context.Background(), "jobs/123/")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deletedKeys) != 2 {
		t.Fatalf("expected 2 deleted keys, got %d", len(deletedKeys))
	}
	if deletedKeys[0] != "jobs/123/a.pdf" || deletedKeys[1] != "jobs/123/b.pdf" {
		t.Errorf("deleted keys = %v, want [jobs/123/a.pdf, jobs/123/b.pdf]", deletedKeys)
	}
}

func TestDeleteByPrefix_MultiplePages(t *testing.T) {
	callCount := 0

	mock := &mockS3{
		listObjectsV2Fn: func(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			callCount++
			switch callCount {
			case 1:
				return &s3.ListObjectsV2Output{
					Contents: []types.Object{
						{Key: aws.String("prefix/obj1")},
					},
					IsTruncated:           aws.Bool(true),
					NextContinuationToken: aws.String("token-2"),
				}, nil
			case 2:
				if aws.ToString(params.ContinuationToken) != "token-2" {
					t.Errorf("continuation token = %q, want %q", aws.ToString(params.ContinuationToken), "token-2")
				}
				return &s3.ListObjectsV2Output{
					Contents: []types.Object{
						{Key: aws.String("prefix/obj2")},
					},
					IsTruncated: aws.Bool(false),
				}, nil
			default:
				t.Fatal("unexpected list call")
				return nil, nil
			}
		},
		deleteObjectsFn: func(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
			return &s3.DeleteObjectsOutput{}, nil
		},
	}

	client := newClientWithS3(mock, testBucket)
	err := client.DeleteByPrefix(context.Background(), "prefix/")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 list calls, got %d", callCount)
	}
}

func TestDeleteByPrefix_ListError(t *testing.T) {
	mock := &mockS3{
		listObjectsV2Fn: func(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			return nil, &apiError{code: "NoSuchBucket", message: "bucket gone"}
		},
	}

	client := newClientWithS3(mock, testBucket)
	err := client.DeleteByPrefix(context.Background(), "prefix/")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if !strings.Contains(err.Error(), "DeleteByPrefix/List") {
		t.Errorf("error should mention operation, got %q", err.Error())
	}
}

func TestDeleteByPrefix_DeleteObjectsError(t *testing.T) {
	mock := &mockS3{
		listObjectsV2Fn: func(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			return &s3.ListObjectsV2Output{
				Contents:    []types.Object{{Key: aws.String("prefix/obj1")}},
				IsTruncated: aws.Bool(false),
			}, nil
		},
		deleteObjectsFn: func(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
			return nil, &apiError{code: "InternalError", message: "delete failed"}
		},
	}

	client := newClientWithS3(mock, testBucket)
	err := client.DeleteByPrefix(context.Background(), "prefix/")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if !strings.Contains(err.Error(), "DeleteByPrefix/Delete") {
		t.Errorf("error should mention operation, got %q", err.Error())
	}
}

func TestDeleteByPrefix_ContextCancelledBetweenPages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0

	mock := &mockS3{
		listObjectsV2Fn: func(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			callCount++
			if callCount == 1 {
				return &s3.ListObjectsV2Output{
					Contents: []types.Object{
						{Key: aws.String("prefix/obj1")},
					},
					IsTruncated:           aws.Bool(true),
					NextContinuationToken: aws.String("token-2"),
				}, nil
			}
			t.Fatal("should not reach second list call")
			return nil, nil
		},
		deleteObjectsFn: func(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
			cancel() // cancel context after first batch delete
			return &s3.DeleteObjectsOutput{}, nil
		},
	}

	client := newClientWithS3(mock, testBucket)
	err := client.DeleteByPrefix(ctx, "prefix/")

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// --- Interface Compliance ---

func TestInterfaceCompliance(t *testing.T) {
	var _ port.TempStoragePort = (*Client)(nil)
	var _ S3API = (*mockS3)(nil)
}

// --- Error Mapping Tests ---

func TestMapError_ContextCanceled(t *testing.T) {
	err := mapError(context.Canceled, "Upload")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	// Must NOT be wrapped in DomainError.
	if port.IsDomainError(err) {
		t.Error("context.Canceled should not be wrapped in DomainError")
	}
}

func TestMapError_ContextDeadlineExceeded(t *testing.T) {
	err := mapError(context.DeadlineExceeded, "Download")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.DeadlineExceeded should not be wrapped in DomainError")
	}
}

func TestMapError_NoSuchKey(t *testing.T) {
	original := &types.NoSuchKey{Message: aws.String("not found")}
	err := mapError(original, "Download")

	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if port.IsRetryable(err) {
		t.Error("NoSuchKey should not be retryable")
	}
	if !strings.Contains(err.Error(), "NoSuchKey") {
		t.Errorf("error should contain 'NoSuchKey', got %q", err.Error())
	}
}

func TestMapError_APIError(t *testing.T) {
	original := &apiError{code: "NoSuchBucket", message: "bucket not found"}
	err := mapError(original, "Upload")

	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if !strings.Contains(err.Error(), "NoSuchBucket") {
		t.Errorf("error should contain 'NoSuchBucket', got %q", err.Error())
	}
}

func TestMapError_UnknownError(t *testing.T) {
	original := errors.New("connection reset")
	err := mapError(original, "Delete")

	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("unknown error should be retryable (storage errors are retryable)")
	}
	if !strings.Contains(err.Error(), "objectstorage: Delete") {
		t.Errorf("error should contain operation, got %q", err.Error())
	}
}

// --- Download nil Body ---

func TestDownload_NilBody(t *testing.T) {
	mock := &mockS3{
		getObjectFn: func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			return &s3.GetObjectOutput{Body: nil}, nil
		},
	}

	client := newClientWithS3(mock, testBucket)
	_, err := client.Download(context.Background(), "nil-body-key")

	if err == nil {
		t.Fatal("expected error for nil body, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if !strings.Contains(err.Error(), "nil body") {
		t.Errorf("error should mention nil body, got %q", err.Error())
	}
}

// --- DeleteByPrefix empty prefix ---

func TestDeleteByPrefix_EmptyPrefix(t *testing.T) {
	client := newClientWithS3(&mockS3{}, testBucket)
	err := client.DeleteByPrefix(context.Background(), "")

	if err == nil {
		t.Fatal("expected error for empty prefix, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if !strings.Contains(err.Error(), "empty prefix") {
		t.Errorf("error should mention empty prefix, got %q", err.Error())
	}
}

// --- DeleteByPrefix partial delete errors ---

func TestDeleteByPrefix_PartialDeleteError(t *testing.T) {
	mock := &mockS3{
		listObjectsV2Fn: func(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			return &s3.ListObjectsV2Output{
				Contents:    []types.Object{{Key: aws.String("prefix/obj1")}},
				IsTruncated: aws.Bool(false),
			}, nil
		},
		deleteObjectsFn: func(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
			return &s3.DeleteObjectsOutput{
				Errors: []types.Error{
					{
						Key:     aws.String("prefix/obj1"),
						Code:    aws.String("AccessDenied"),
						Message: aws.String("permission denied"),
					},
				},
			}, nil
		},
	}

	client := newClientWithS3(mock, testBucket)
	err := client.DeleteByPrefix(context.Background(), "prefix/")

	if err == nil {
		t.Fatal("expected error for partial delete failure, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if !strings.Contains(err.Error(), "1 objects failed") {
		t.Errorf("error should mention failed count, got %q", err.Error())
	}
}

// --- mapError retryable API error ---

func TestMapError_RetryableAPIError(t *testing.T) {
	original := &apiError{code: "InternalError", message: "server error"}
	err := mapError(original, "Upload")

	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("InternalError should be retryable")
	}
}

func TestMapError_AccessDenied(t *testing.T) {
	original := &apiError{code: "AccessDenied", message: "forbidden"}
	err := mapError(original, "Download")

	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if port.IsRetryable(err) {
		t.Error("AccessDenied should not be retryable")
	}
}

// --- Verify body passthrough for Upload ---

func TestUpload_BodyPassthrough(t *testing.T) {
	largeData := bytes.Repeat([]byte("A"), 1024*1024) // 1 MB

	mock := &mockS3{
		putObjectFn: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			got, err := io.ReadAll(params.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if !bytes.Equal(got, largeData) {
				t.Errorf("body length = %d, want %d", len(got), len(largeData))
			}
			return &s3.PutObjectOutput{}, nil
		},
	}

	client := newClientWithS3(mock, testBucket)
	err := client.Upload(context.Background(), "big-file", bytes.NewReader(largeData))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
