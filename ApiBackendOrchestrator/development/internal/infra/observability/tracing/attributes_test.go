package tracing

import (
	"testing"
)

func TestHTTPRequestAttrs_AllFields(t *testing.T) {
	attrs := HTTPRequestAttrs("POST", "/api/v1/contracts", 201, "corr-1", "org-1", "user-1")

	// 3 standard + 3 orchestrator = 6
	if len(attrs) != 6 {
		t.Errorf("expected 6 attributes, got %d", len(attrs))
	}

	expected := map[string]string{
		"http.method":          "POST",
		"http.route":           "/api/v1/contracts",
		"orch.correlation_id":  "corr-1",
		"orch.organization_id": "org-1",
		"orch.user_id":         "user-1",
	}
	for key, val := range expected {
		found := false
		for _, attr := range attrs {
			if string(attr.Key) == key && attr.Value.AsString() == val {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing attribute %s=%s", key, val)
		}
	}

	// Check status_code.
	found := false
	for _, attr := range attrs {
		if string(attr.Key) == "http.status_code" && attr.Value.AsInt64() == 201 {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing attribute http.status_code=201")
	}
}

func TestHTTPRequestAttrs_EmptyOptionalFields(t *testing.T) {
	attrs := HTTPRequestAttrs("GET", "/healthz", 200, "", "", "")

	// 3 standard attrs only (empty strings are not appended).
	if len(attrs) != 3 {
		t.Errorf("expected 3 attributes (no empty optional), got %d", len(attrs))
	}
}

func TestDMRequestAttrs_AllFields(t *testing.T) {
	attrs := DMRequestAttrs("CreateDocument", "POST", "/api/v1/documents", 201, 2)

	if len(attrs) != 5 {
		t.Errorf("expected 5 attributes, got %d", len(attrs))
	}

	found := false
	for _, attr := range attrs {
		if string(attr.Key) == "orch.dm.operation" && attr.Value.AsString() == "CreateDocument" {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing attribute orch.dm.operation=CreateDocument")
	}

	found = false
	for _, attr := range attrs {
		if string(attr.Key) == "orch.dm.retry_count" && attr.Value.AsInt64() == 2 {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing attribute orch.dm.retry_count=2")
	}
}

func TestBrokerPublishAttrs_AllFields(t *testing.T) {
	attrs := BrokerPublishAttrs("dp.commands.process-document", "ProcessDocumentRequested")

	if len(attrs) != 3 {
		t.Errorf("expected 3 attributes, got %d", len(attrs))
	}

	expected := map[string]string{
		"messaging.system":      "rabbitmq",
		"messaging.destination":  "dp.commands.process-document",
		"orch.event_type":       "ProcessDocumentRequested",
	}
	for key, val := range expected {
		found := false
		for _, attr := range attrs {
			if string(attr.Key) == key && attr.Value.AsString() == val {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing attribute %s=%s", key, val)
		}
	}
}

func TestS3UploadAttrs_AllFields(t *testing.T) {
	attrs := S3UploadAttrs("my-bucket", "uploads/file.pdf", 1048576)

	if len(attrs) != 3 {
		t.Errorf("expected 3 attributes, got %d", len(attrs))
	}

	found := false
	for _, attr := range attrs {
		if string(attr.Key) == "orch.s3.bucket" && attr.Value.AsString() == "my-bucket" {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing attribute orch.s3.bucket=my-bucket")
	}

	found = false
	for _, attr := range attrs {
		if string(attr.Key) == "orch.s3.content_length" && attr.Value.AsInt64() == 1048576 {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing attribute orch.s3.content_length=1048576")
	}
}

func TestSpanNameConstants(t *testing.T) {
	names := []string{
		SpanHTTPRequest,
		SpanDMRequest,
		SpanBrokerPublish,
		SpanUploadS3,
		SpanRedisOperation,
		SpanEventProcess,
		SpanOPMRequest,
		SpanAuthValidate,
		SpanRBACCheck,
		SpanRateLimitCheck,
	}
	for _, name := range names {
		if name == "" {
			t.Error("span name constant should not be empty")
		}
	}
	if len(names) != 10 {
		t.Errorf("expected 10 span name constants, got %d", len(names))
	}
}

func TestAttributeKeys_NonEmpty(t *testing.T) {
	keys := []string{
		string(AttrCorrelationID),
		string(AttrOrganizationID),
		string(AttrUserID),
		string(AttrDocumentID),
		string(AttrVersionID),
		string(AttrJobID),
		string(AttrEventType),
		string(AttrDMOperation),
		string(AttrDMRetryCount),
		string(AttrDMCircuitBreakerState),
		string(AttrS3Bucket),
		string(AttrS3Key),
		string(AttrS3ContentLength),
		string(AttrMessagingSystem),
		string(AttrMessagingDestination),
	}
	for _, k := range keys {
		if k == "" {
			t.Error("attribute key should not be empty")
		}
	}
}
