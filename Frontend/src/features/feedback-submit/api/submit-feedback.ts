// POST /contracts/{contract_id}/versions/{version_id}/feedback (§7.5 api-specification).
//
// Контракт: POST с телом FeedbackRequest {is_useful, comment?} → 201 FeedbackResponse
// {feedback_id, created_at}. Тонкая обёртка: вызывает axios, сужает тип ответа.
// Ошибки 400/401/404 прокидываются как OrchestratorError через axios-interceptor.
import type { components } from '@/shared/api/openapi';

import type { SubmitFeedbackInput, SubmitFeedbackResponse } from '../model/types';
import { getHttpInstance } from './http';

function endpointFor(contractId: string, versionId: string): string {
  return `/contracts/${encodeURIComponent(contractId)}/versions/${encodeURIComponent(versionId)}/feedback`;
}

type RawRequest = components['schemas']['FeedbackRequest'];
type RawResponse = components['schemas']['FeedbackResponse'];

function narrowResponse(raw: RawResponse): SubmitFeedbackResponse {
  const { feedback_id, created_at } = raw;
  if (typeof feedback_id !== 'string' || typeof created_at !== 'string') {
    throw new Error('FeedbackResponse: пришёл 201, но обязательные поля отсутствуют.');
  }
  return { feedbackId: feedback_id, createdAt: created_at };
}

export interface SubmitFeedbackOptions {
  signal?: AbortSignal;
}

/**
 * Отправляет отзыв пользователя о результатах проверки (UR-11).
 * POST с JSON-телом; 201 возвращает идентификатор созданной записи.
 */
export async function submitFeedback(
  input: SubmitFeedbackInput,
  opts: SubmitFeedbackOptions = {},
): Promise<SubmitFeedbackResponse> {
  const http = getHttpInstance();
  const body: RawRequest = {
    is_useful: input.isUseful,
    ...(input.comment !== undefined && { comment: input.comment }),
  };
  const { data } = await http.post<RawResponse>(
    endpointFor(input.contractId, input.versionId),
    body,
    {
      ...(opts.signal && { signal: opts.signal }),
    },
  );
  return narrowResponse(data);
}

export { endpointFor as submitFeedbackEndpoint };
