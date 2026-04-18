// POST /contracts/{contract_id}/compare (§7.5 api-specification).
//
// Контракт: body {base_version_id, target_version_id} → 202 CompareResponse
// {job_id, status}. Бэкенд публикует ComparisonRequested в RabbitMQ и сразу
// возвращает 202 — таймаут дефолтный (не нужно 120s: сам compute идёт асинхронно).
//
// Тонкая обёртка: сериализует input, вызывает axios, сужает тип ответа.
// 409 VERSION_STILL_PROCESSING прокидывается как OrchestratorError — маппер на
// UX-toast делает хук через `toUserMessage`.
import type { components } from '@/shared/api/openapi';

import type {
  StartComparisonInput,
  StartComparisonResponse,
} from '../model/types';
import { getHttpInstance } from './http';

function endpointFor(contractId: string): string {
  return `/contracts/${encodeURIComponent(contractId)}/compare`;
}

export interface StartComparisonOptions {
  signal?: AbortSignal;
}

type RawRequest = components['schemas']['CompareRequest'];
type RawResponse = components['schemas']['CompareResponse'];

/**
 * Проверяет, что 202-ответ содержит обязательные поля. OpenAPI-схема помечает
 * их optional (open set), но backend по контракту всегда возвращает — защита
 * от drift'а.
 */
function narrowResponse(raw: RawResponse): StartComparisonResponse {
  const { job_id, status } = raw;
  if (typeof job_id !== 'string' || typeof status !== 'string') {
    throw new Error('CompareResponse: пришёл 202, но обязательные поля отсутствуют.');
  }
  return { jobId: job_id, status };
}

/**
 * Запускает сравнение двух версий договора (UR-7). Возвращает job_id и
 * исходный статус (обычно QUEUED). Прогресс и завершение приходят через SSE.
 */
export async function startComparison(
  input: StartComparisonInput,
  opts: StartComparisonOptions = {},
): Promise<StartComparisonResponse> {
  const body: RawRequest = {
    base_version_id: input.baseVersionId,
    target_version_id: input.targetVersionId,
  };

  const http = getHttpInstance();
  const { data } = await http.post<RawResponse>(endpointFor(input.contractId), body, {
    ...(opts.signal && { signal: opts.signal }),
  });

  return narrowResponse(data);
}

export { endpointFor as startComparisonEndpoint };
