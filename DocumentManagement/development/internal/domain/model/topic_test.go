package model

import (
	"testing"
)

func TestIncomingTopicConstants(t *testing.T) {
	topics := []struct {
		name  string
		value string
	}{
		{"TopicDPArtifactsProcessingReady", TopicDPArtifactsProcessingReady},
		{"TopicDPRequestsSemanticTree", TopicDPRequestsSemanticTree},
		{"TopicDPArtifactsDiffReady", TopicDPArtifactsDiffReady},
		{"TopicLICArtifactsAnalysisReady", TopicLICArtifactsAnalysisReady},
		{"TopicLICRequestsArtifacts", TopicLICRequestsArtifacts},
		{"TopicREArtifactsReportsReady", TopicREArtifactsReportsReady},
		{"TopicRERequestsArtifacts", TopicRERequestsArtifacts},
	}

	for _, tc := range topics {
		if tc.value == "" {
			t.Errorf("%s should not be empty", tc.name)
		}
	}

	if len(topics) != 7 {
		t.Errorf("expected 7 incoming topics, got %d", len(topics))
	}
}

func TestOutgoingConfirmationTopicConstants(t *testing.T) {
	topics := []struct {
		name  string
		value string
	}{
		{"TopicDMResponsesArtifactsPersisted", TopicDMResponsesArtifactsPersisted},
		{"TopicDMResponsesArtifactsPersistFailed", TopicDMResponsesArtifactsPersistFailed},
		{"TopicDMResponsesSemanticTreeProvided", TopicDMResponsesSemanticTreeProvided},
		{"TopicDMResponsesArtifactsProvided", TopicDMResponsesArtifactsProvided},
		{"TopicDMResponsesDiffPersisted", TopicDMResponsesDiffPersisted},
		{"TopicDMResponsesDiffPersistFailed", TopicDMResponsesDiffPersistFailed},
		{"TopicDMResponsesLICArtifactsPersisted", TopicDMResponsesLICArtifactsPersisted},
		{"TopicDMResponsesLICArtifactsPersistFailed", TopicDMResponsesLICArtifactsPersistFailed},
		{"TopicDMResponsesREReportsPersisted", TopicDMResponsesREReportsPersisted},
		{"TopicDMResponsesREReportsPersistFailed", TopicDMResponsesREReportsPersistFailed},
	}

	for _, tc := range topics {
		if tc.value == "" {
			t.Errorf("%s should not be empty", tc.name)
		}
	}

	if len(topics) != 10 {
		t.Errorf("expected 10 confirmation topics, got %d", len(topics))
	}
}

func TestOutgoingNotificationTopicConstants(t *testing.T) {
	topics := []struct {
		name  string
		value string
	}{
		{"TopicDMEventsVersionArtifactsReady", TopicDMEventsVersionArtifactsReady},
		{"TopicDMEventsVersionAnalysisReady", TopicDMEventsVersionAnalysisReady},
		{"TopicDMEventsVersionReportsReady", TopicDMEventsVersionReportsReady},
		{"TopicDMEventsVersionCreated", TopicDMEventsVersionCreated},
		{"TopicDMEventsVersionPartiallyAvailable", TopicDMEventsVersionPartiallyAvailable},
	}

	for _, tc := range topics {
		if tc.value == "" {
			t.Errorf("%s should not be empty", tc.name)
		}
	}

	if len(topics) != 5 {
		t.Errorf("expected 5 notification topics, got %d", len(topics))
	}
}

func TestDLQTopicConstants(t *testing.T) {
	topics := []struct {
		name  string
		value string
	}{
		{"TopicDMDLQIngestionFailed", TopicDMDLQIngestionFailed},
		{"TopicDMDLQQueryFailed", TopicDMDLQQueryFailed},
		{"TopicDMDLQInvalidMessage", TopicDMDLQInvalidMessage},
	}

	for _, tc := range topics {
		if tc.value == "" {
			t.Errorf("%s should not be empty", tc.name)
		}
	}

	if len(topics) != 3 {
		t.Errorf("expected 3 DLQ topics, got %d", len(topics))
	}
}

func TestTopicNamingConvention(t *testing.T) {
	// Verify DM topics follow dm.{type}.{action} pattern.
	dmTopics := []string{
		TopicDMResponsesArtifactsPersisted,
		TopicDMResponsesArtifactsPersistFailed,
		TopicDMResponsesSemanticTreeProvided,
		TopicDMResponsesArtifactsProvided,
		TopicDMResponsesDiffPersisted,
		TopicDMResponsesDiffPersistFailed,
		TopicDMResponsesLICArtifactsPersisted,
		TopicDMResponsesLICArtifactsPersistFailed,
		TopicDMResponsesREReportsPersisted,
		TopicDMResponsesREReportsPersistFailed,
		TopicDMEventsVersionArtifactsReady,
		TopicDMEventsVersionAnalysisReady,
		TopicDMEventsVersionReportsReady,
		TopicDMEventsVersionCreated,
		TopicDMEventsVersionPartiallyAvailable,
		TopicDMDLQIngestionFailed,
		TopicDMDLQQueryFailed,
		TopicDMDLQInvalidMessage,
	}

	for _, topic := range dmTopics {
		if len(topic) < 4 || topic[:3] != "dm." {
			t.Errorf("DM topic %q should start with 'dm.'", topic)
		}
	}
}
