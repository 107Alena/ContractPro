// POST /contracts/{contract_id}/versions/{version_id}/recheck (§7.5 api-specification).
//
// Контракт: POST БЕЗ тела → 202 UploadResponse (новая версия с
// origin_type=RE_CHECK, тот же файл, новый анализ). Тонкая обёртка: вызывает
// axios, сужает тип ответа. 409 VERSION_STILL_PROCESSING прокидывается как
// OrchestratorError — маппер на UX-toast делает хук через `toUserMessage`.
import type { components } from '@/shared/api/openapi';

import type {
  RecheckVersionInput,
  RecheckVersionResponse,
} from '../model/types';
import { getHttpInstance } from './http';

function endpointFor(contractId: string, versionId: string): string {
  return `/contracts/${encodeURIComponent(contractId)}/versions/${encodeURIComponent(versionId)}/recheck`;
}

export interface RecheckVersionOptions {
  signal?: AbortSignal;
}

type RawResponse = components['schemas']['UploadResponse'];

function narrowResponse(raw: RawResponse): RecheckVersionResponse {
  const { contract_id, version_id, version_number, job_id, status, message } = raw;
  if (
    typeof contract_id !== 'string' ||
    typeof version_id !== 'string' ||
    typeof version_number !== 'number' ||
    typeof job_id !== 'string' ||
    typeof status !== 'string'
  ) {
    throw new Error('UploadResponse: пришёл 202, но обязательные поля отсутствуют.');
  }
  const result: RecheckVersionResponse = {
    contractId: contract_id,
    versionId: version_id,
    versionNumber: version_number,
    jobId: job_id,
    status,
  };
  if (typeof message === 'string') result.message = message;
  return result;
}

/**
 * Запускает повторную проверку существующей версии (UR-9). POST без тела —
 * параметры берутся из path. Таймаут дефолтный (не нужно 120s: backend
 * публикует команду в RabbitMQ и возвращает 202 быстро).
 */
export async function recheckVersion(
  input: RecheckVersionInput,
  opts: RecheckVersionOptions = {},
): Promise<RecheckVersionResponse> {
  const http = getHttpInstance();
  const { data } = await http.post<RawResponse>(
    endpointFor(input.contractId, input.versionId),
    // body=undefined для POST без тела. Axios при undefined не выставляет
    // Content-Type и не сериализует payload — нужное поведение для 202 endpoint'а.
    undefined,
    {
      ...(opts.signal && { signal: opts.signal }),
    },
  );

  return narrowResponse(data);
}

export { endpointFor as recheckVersionEndpoint };
