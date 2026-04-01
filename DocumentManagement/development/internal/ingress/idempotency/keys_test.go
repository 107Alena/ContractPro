package idempotency

import (
	"testing"

	"contractpro/document-management/internal/domain/model"
)

func TestKeyForDPArtifacts(t *testing.T) {
	key := KeyForDPArtifacts("job-abc-123")
	want := "dm:idem:dp-art:job-abc-123"
	if key != want {
		t.Errorf("KeyForDPArtifacts = %q, want %q", key, want)
	}
}

func TestKeyForSemanticTreeRequest(t *testing.T) {
	key := KeyForSemanticTreeRequest("job-abc", "ver-456")
	want := "dm:idem:dp-tree:job-abc:ver-456"
	if key != want {
		t.Errorf("KeyForSemanticTreeRequest = %q, want %q", key, want)
	}
}

func TestKeyForDiffReady(t *testing.T) {
	key := KeyForDiffReady("job-diff-1")
	want := "dm:idem:dp-diff:job-diff-1"
	if key != want {
		t.Errorf("KeyForDiffReady = %q, want %q", key, want)
	}
}

func TestKeyForLICArtifacts(t *testing.T) {
	key := KeyForLICArtifacts("job-lic-99")
	want := "dm:idem:lic-art:job-lic-99"
	if key != want {
		t.Errorf("KeyForLICArtifacts = %q, want %q", key, want)
	}
}

func TestKeyForLICRequest(t *testing.T) {
	key := KeyForLICRequest("job-lic-req", "ver-789")
	want := "dm:idem:lic-req:job-lic-req:ver-789"
	if key != want {
		t.Errorf("KeyForLICRequest = %q, want %q", key, want)
	}
}

func TestKeyForREArtifacts(t *testing.T) {
	key := KeyForREArtifacts("job-re-42")
	want := "dm:idem:re-art:job-re-42"
	if key != want {
		t.Errorf("KeyForREArtifacts = %q, want %q", key, want)
	}
}

func TestKeyForRERequest(t *testing.T) {
	key := KeyForRERequest("job-re-req", "ver-111")
	want := "dm:idem:re-req:job-re-req:ver-111"
	if key != want {
		t.Errorf("KeyForRERequest = %q, want %q", key, want)
	}
}

func TestKeyUniqueness_DifferentTopics_SameJobID(t *testing.T) {
	jobID := "same-job-id"
	keys := map[string]bool{
		KeyForDPArtifacts(jobID):  true,
		KeyForDiffReady(jobID):    true,
		KeyForLICArtifacts(jobID): true,
		KeyForREArtifacts(jobID):  true,
	}
	if len(keys) != 4 {
		t.Errorf("expected 4 unique keys for same job_id on different topics, got %d", len(keys))
	}
}

func TestKeyUniqueness_RequestTopics_SameJobDifferentVersion(t *testing.T) {
	jobID := "same-job"
	key1 := KeyForSemanticTreeRequest(jobID, "ver-1")
	key2 := KeyForSemanticTreeRequest(jobID, "ver-2")
	if key1 == key2 {
		t.Error("expected different keys for same job but different versions")
	}
}

func TestTopicShortName_AllTopics(t *testing.T) {
	tests := []struct {
		topic string
		want  string
	}{
		{model.TopicDPArtifactsProcessingReady, "dp-art"},
		{model.TopicDPRequestsSemanticTree, "dp-tree"},
		{model.TopicDPArtifactsDiffReady, "dp-diff"},
		{model.TopicLICArtifactsAnalysisReady, "lic-art"},
		{model.TopicLICRequestsArtifacts, "lic-req"},
		{model.TopicREArtifactsReportsReady, "re-art"},
		{model.TopicRERequestsArtifacts, "re-req"},
	}
	for _, tt := range tests {
		got := TopicShortName(tt.topic)
		if got != tt.want {
			t.Errorf("TopicShortName(%q) = %q, want %q", tt.topic, got, tt.want)
		}
	}
}

func TestTopicShortName_UnknownTopic(t *testing.T) {
	got := TopicShortName("unknown.topic")
	if got != "" {
		t.Errorf("TopicShortName(unknown) = %q, want empty", got)
	}
}

func TestTopicShortName_Coverage_All7Topics(t *testing.T) {
	if len(topicShortNames) != 7 {
		t.Errorf("topicShortNames has %d entries, want 7 (one per incoming topic)", len(topicShortNames))
	}
}

// ---------------------------------------------------------------------------
// Key validation: panics on empty IDs
// ---------------------------------------------------------------------------

func TestKeyForDPArtifacts_PanicOnEmptyJobID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty jobID")
		}
	}()
	KeyForDPArtifacts("")
}

func TestKeyForSemanticTreeRequest_PanicOnEmptyJobID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty jobID")
		}
	}()
	KeyForSemanticTreeRequest("", "ver-1")
}

func TestKeyForSemanticTreeRequest_PanicOnEmptyVersionID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty versionID")
		}
	}()
	KeyForSemanticTreeRequest("job-1", "")
}

func TestKeyForDiffReady_PanicOnEmptyJobID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty jobID")
		}
	}()
	KeyForDiffReady("")
}

func TestKeyForLICArtifacts_PanicOnEmptyJobID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty jobID")
		}
	}()
	KeyForLICArtifacts("")
}

func TestKeyForREArtifacts_PanicOnEmptyJobID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty jobID")
		}
	}()
	KeyForREArtifacts("")
}

func TestKeyForLICRequest_PanicOnEmptyJobID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty jobID")
		}
	}()
	KeyForLICRequest("", "ver-1")
}

func TestKeyForRERequest_PanicOnEmptyVersionID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty versionID")
		}
	}()
	KeyForRERequest("job-1", "")
}
