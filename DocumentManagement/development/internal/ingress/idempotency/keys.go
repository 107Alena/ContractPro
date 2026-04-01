package idempotency

import (
	"fmt"

	"contractpro/document-management/internal/domain/model"
)

// Idempotency key prefix.
const keyPrefix = "dm:idem"

// topicShortNames maps incoming topic names to short prefixes for key generation.
// Short prefixes prevent key collisions between events from different topics
// that share the same job_id.
var topicShortNames = map[string]string{
	model.TopicDPArtifactsProcessingReady: "dp-art",
	model.TopicDPRequestsSemanticTree:     "dp-tree",
	model.TopicDPArtifactsDiffReady:       "dp-diff",
	model.TopicLICArtifactsAnalysisReady:  "lic-art",
	model.TopicLICRequestsArtifacts:       "lic-req",
	model.TopicREArtifactsReportsReady:    "re-art",
	model.TopicRERequestsArtifacts:        "re-req",
}

// KeyForDPArtifacts generates an idempotency key for DocumentProcessingArtifactsReady.
// Keyed by job_id alone — a single job produces exactly one set of DP artifacts.
// Panics on empty jobID (programming error — events must be validated upstream).
func KeyForDPArtifacts(jobID string) string {
	mustNotEmpty("jobID", jobID)
	return fmt.Sprintf("%s:dp-art:%s", keyPrefix, jobID)
}

// KeyForSemanticTreeRequest generates an idempotency key for GetSemanticTreeRequest.
// Keyed by job_id + version_id — the same job might request trees for different versions
// (comparison pipeline requests two trees).
func KeyForSemanticTreeRequest(jobID, versionID string) string {
	mustNotEmpty("jobID", jobID)
	mustNotEmpty("versionID", versionID)
	return fmt.Sprintf("%s:dp-tree:%s:%s", keyPrefix, jobID, versionID)
}

// KeyForDiffReady generates an idempotency key for DocumentVersionDiffReady.
// Keyed by job_id alone — a single job produces exactly one diff result.
func KeyForDiffReady(jobID string) string {
	mustNotEmpty("jobID", jobID)
	return fmt.Sprintf("%s:dp-diff:%s", keyPrefix, jobID)
}

// KeyForLICArtifacts generates an idempotency key for LegalAnalysisArtifactsReady.
// Keyed by job_id alone — a single job produces exactly one set of LIC artifacts.
func KeyForLICArtifacts(jobID string) string {
	mustNotEmpty("jobID", jobID)
	return fmt.Sprintf("%s:lic-art:%s", keyPrefix, jobID)
}

// KeyForLICRequest generates an idempotency key for GetArtifactsRequest from LIC.
// Keyed by job_id + version_id — different jobs may request different versions.
func KeyForLICRequest(jobID, versionID string) string {
	mustNotEmpty("jobID", jobID)
	mustNotEmpty("versionID", versionID)
	return fmt.Sprintf("%s:lic-req:%s:%s", keyPrefix, jobID, versionID)
}

// KeyForREArtifacts generates an idempotency key for ReportsArtifactsReady.
// Keyed by job_id alone — a single job produces exactly one set of RE artifacts.
func KeyForREArtifacts(jobID string) string {
	mustNotEmpty("jobID", jobID)
	return fmt.Sprintf("%s:re-art:%s", keyPrefix, jobID)
}

// KeyForRERequest generates an idempotency key for GetArtifactsRequest from RE.
// Keyed by job_id + version_id — different jobs may request different versions.
func KeyForRERequest(jobID, versionID string) string {
	mustNotEmpty("jobID", jobID)
	mustNotEmpty("versionID", versionID)
	return fmt.Sprintf("%s:re-req:%s:%s", keyPrefix, jobID, versionID)
}

// TopicShortName returns the short prefix for a given topic.
// Returns empty string for unknown topics.
func TopicShortName(topic string) string {
	return topicShortNames[topic]
}

// mustNotEmpty panics if value is empty. Key generation with empty IDs is a
// programming error — events must be validated before reaching the idempotency guard.
func mustNotEmpty(name, value string) {
	if value == "" {
		panic(fmt.Sprintf("idempotency: %s must not be empty", name))
	}
}
