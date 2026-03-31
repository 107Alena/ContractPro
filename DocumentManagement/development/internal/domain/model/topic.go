package model

// Incoming topics — DM subscribes to these queues.
const (
	TopicDPArtifactsProcessingReady = "dp.artifacts.processing-ready"
	TopicDPRequestsSemanticTree     = "dp.requests.semantic-tree"
	TopicDPArtifactsDiffReady       = "dp.artifacts.diff-ready"
	TopicLICArtifactsAnalysisReady  = "lic.artifacts.analysis-ready"
	TopicLICRequestsArtifacts       = "lic.requests.artifacts"
	TopicREArtifactsReportsReady    = "re.artifacts.reports-ready"
	TopicRERequestsArtifacts        = "re.requests.artifacts"
)

// Outgoing confirmation topics — DM publishes responses to senders.
const (
	TopicDMResponsesArtifactsPersisted     = "dm.responses.artifacts-persisted"
	TopicDMResponsesArtifactsPersistFailed = "dm.responses.artifacts-persist-failed"
	TopicDMResponsesSemanticTreeProvided   = "dm.responses.semantic-tree-provided"
	TopicDMResponsesArtifactsProvided      = "dm.responses.artifacts-provided"
	TopicDMResponsesDiffPersisted          = "dm.responses.diff-persisted"
	TopicDMResponsesDiffPersistFailed      = "dm.responses.diff-persist-failed"
	TopicDMResponsesLICArtifactsPersisted  = "dm.responses.lic-artifacts-persisted"
	TopicDMResponsesLICArtifactsPersistFailed = "dm.responses.lic-artifacts-persist-failed"
	TopicDMResponsesREReportsPersisted     = "dm.responses.re-reports-persisted"
	TopicDMResponsesREReportsPersistFailed = "dm.responses.re-reports-persist-failed"
)

// Outgoing notification topics — DM publishes events for downstream domains.
const (
	TopicDMEventsVersionArtifactsReady  = "dm.events.version-artifacts-ready"
	TopicDMEventsVersionAnalysisReady   = "dm.events.version-analysis-ready"
	TopicDMEventsVersionReportsReady    = "dm.events.version-reports-ready"
	TopicDMEventsVersionCreated         = "dm.events.version-created"
	TopicDMEventsVersionPartiallyAvailable = "dm.events.version-partially-available"
)

// DLQ topics — failed messages for post-mortem analysis.
const (
	TopicDMDLQIngestionFailed = "dm.dlq.ingestion-failed"
	TopicDMDLQQueryFailed     = "dm.dlq.query-failed"
	TopicDMDLQInvalidMessage  = "dm.dlq.invalid-message"
)
