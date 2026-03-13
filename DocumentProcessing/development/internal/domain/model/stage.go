package model

// ProcessingStage represents a stage in the document processing pipeline.
type ProcessingStage string

const (
	ProcessingStageValidatingInput    ProcessingStage = "VALIDATING_INPUT"
	ProcessingStageFetchingSourceFile ProcessingStage = "FETCHING_SOURCE_FILE"
	ProcessingStageValidatingFile     ProcessingStage = "VALIDATING_FILE"
	ProcessingStageOCR                ProcessingStage = "OCR"
	ProcessingStageOCRSkipped         ProcessingStage = "OCR_SKIPPED"
	ProcessingStageTextExtraction     ProcessingStage = "TEXT_EXTRACTION"
	ProcessingStageStructureExtract   ProcessingStage = "STRUCTURE_EXTRACTION"
	ProcessingStageSemanticTree       ProcessingStage = "SEMANTIC_TREE_BUILDING"
	ProcessingStageSavingArtifacts    ProcessingStage = "SAVING_ARTIFACTS"
	ProcessingStageWaitingDM          ProcessingStage = "WAITING_DM_CONFIRMATION"
	ProcessingStageCleanup            ProcessingStage = "CLEANUP_TEMP_ARTIFACTS"
)

// ComparisonStage represents a stage in the version comparison pipeline.
type ComparisonStage string

const (
	ComparisonStageValidatingInput ComparisonStage = "VALIDATING_INPUT"
	ComparisonStageRequestingTrees ComparisonStage = "REQUESTING_SEMANTIC_TREES"
	ComparisonStageWaitingDM       ComparisonStage = "WAITING_DM_RESPONSE"
	ComparisonStageExecutingDiff   ComparisonStage = "EXECUTING_DIFF"
	ComparisonStageSavingResult    ComparisonStage = "SAVING_COMPARISON_RESULT"
	ComparisonStageWaitingConfirm  ComparisonStage = "WAITING_DM_CONFIRMATION"
)
