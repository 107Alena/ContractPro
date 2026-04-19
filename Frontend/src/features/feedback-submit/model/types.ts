// Доменные типы feature feedback-submit.
//
// Endpoint: POST /contracts/{contract_id}/versions/{version_id}/feedback (UR-11).
// Request: FeedbackRequest {is_useful: boolean, comment?: string}.
// Response 201: FeedbackResponse {feedback_id?: uuid, created_at?: ISO}.
// Narrow non-null через runtime-guard.
import type { components } from '@/shared/api/openapi';

type FeedbackRequestRaw = components['schemas']['FeedbackRequest'];
type FeedbackResponseRaw = components['schemas']['FeedbackResponse'];

export interface SubmitFeedbackInput {
  contractId: string;
  versionId: string;
  isUseful: boolean;
  comment?: string;
}

export interface SubmitFeedbackResponse {
  feedbackId: string;
  createdAt: string;
}

/** @internal — type-level smoke-test совместимости с OpenAPI. */
export type __FeedbackRequestRaw = FeedbackRequestRaw;
/** @internal — type-level smoke-test совместимости с OpenAPI. */
export type __FeedbackResponseRaw = FeedbackResponseRaw;
