// Package tracing provides OpenTelemetry tracing for the API/Backend Orchestrator.
//
// When ORCH_TRACING_ENABLED=true and ORCH_TRACING_ENDPOINT is set, traces are
// exported via OTLP/HTTP to a collector (Jaeger, Tempo, Grafana Cloud). When
// disabled, all operations are zero-cost no-ops backed by the OTel noop package.
package tracing

import "go.opentelemetry.io/otel/attribute"

// Span names used across the orchestrator. Each name follows the
// orch.{subsystem}.{operation} convention from observability.md §3.
const (
	SpanHTTPRequest    = "orch.http.request"
	SpanDMRequest      = "orch.dm.request"
	SpanBrokerPublish  = "orch.broker.publish"
	SpanUploadS3       = "orch.upload.s3"
	SpanRedisOperation = "orch.redis.operation"
	SpanEventProcess   = "orch.event.process"
	SpanOPMRequest     = "orch.opm.request"
	SpanAuthValidate   = "orch.auth.validate"
	SpanRBACCheck      = "orch.rbac.check"
	SpanRateLimitCheck = "orch.ratelimit.check"
)

// Attribute keys for orchestrator-specific span attributes.
var (
	// Core orchestrator attributes.
	AttrCorrelationID  = attribute.Key("orch.correlation_id")
	AttrOrganizationID = attribute.Key("orch.organization_id")
	AttrUserID         = attribute.Key("orch.user_id")
	AttrDocumentID     = attribute.Key("orch.document_id")
	AttrVersionID      = attribute.Key("orch.version_id")
	AttrJobID          = attribute.Key("orch.job_id")
	AttrEventType      = attribute.Key("orch.event_type")

	// DM client attributes.
	AttrDMOperation          = attribute.Key("orch.dm.operation")
	AttrDMRetryCount         = attribute.Key("orch.dm.retry_count")
	AttrDMCircuitBreakerState = attribute.Key("orch.dm.circuit_breaker_state")

	// S3 attributes.
	AttrS3Bucket        = attribute.Key("orch.s3.bucket")
	AttrS3Key           = attribute.Key("orch.s3.key")
	AttrS3ContentLength = attribute.Key("orch.s3.content_length")

	// Broker attributes (follow OTel semantic conventions for messaging).
	AttrMessagingSystem      = attribute.Key("messaging.system")
	AttrMessagingDestination = attribute.Key("messaging.destination")
)

// HTTPRequestAttrs returns span attributes for an incoming HTTP request.
func HTTPRequestAttrs(method, routePattern string, statusCode int, correlationID, orgID, userID string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("http.method", method),
		attribute.String("http.route", routePattern),
		attribute.Int("http.status_code", statusCode),
	}
	if correlationID != "" {
		attrs = append(attrs, AttrCorrelationID.String(correlationID))
	}
	if orgID != "" {
		attrs = append(attrs, AttrOrganizationID.String(orgID))
	}
	if userID != "" {
		attrs = append(attrs, AttrUserID.String(userID))
	}
	return attrs
}

// DMRequestAttrs returns span attributes for a DM client HTTP call.
func DMRequestAttrs(operation, method, path string, statusCode, retryCount int) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrDMOperation.String(operation),
		attribute.String("http.method", method),
		attribute.String("http.url", path),
		attribute.Int("http.status_code", statusCode),
		AttrDMRetryCount.Int(retryCount),
	}
}

// BrokerPublishAttrs returns span attributes for a broker publish operation.
func BrokerPublishAttrs(destination, eventType string) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrMessagingSystem.String("rabbitmq"),
		AttrMessagingDestination.String(destination),
		AttrEventType.String(eventType),
	}
}

// S3UploadAttrs returns span attributes for an S3 upload operation.
func S3UploadAttrs(bucket, key string, contentLength int64) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrS3Bucket.String(bucket),
		AttrS3Key.String(key),
		AttrS3ContentLength.Int64(contentLength),
	}
}
