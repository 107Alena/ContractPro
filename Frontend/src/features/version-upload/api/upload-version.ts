// POST /contracts/{contract_id}/versions/upload (§7.5 api-specification, §16.2 high-architecture).
//
// Контракт: multipart/form-data { file } → 202 UploadResponse. В отличие от
// /contracts/upload поле `title` не передаётся — название берётся от уже
// существующего договора. Тонкая обёртка: собирает FormData, вызывает axios,
// сужает тип ответа. Всю логику маппинга ошибок (413/415/INVALID_FILE →
// inline-field) выполняет `useUploadVersion`.
import type { AxiosProgressEvent } from 'axios';

import type { components } from '@/shared/api/openapi';

import type {
  UploadVersionInput,
  UploadVersionProgress,
  UploadVersionResponse,
} from '../model/types';
import { getHttpInstance } from './http';

function endpointFor(contractId: string): string {
  return `/contracts/${encodeURIComponent(contractId)}/versions/upload`;
}

/**
 * Увеличенный таймаут: дефолтный 30s мал для PDF до 20 МБ при медленном канале.
 * Формула §7.5: 120s (как и для первичной загрузки).
 */
const UPLOAD_TIMEOUT_MS = 120_000;

export interface UploadVersionOptions {
  signal?: AbortSignal;
  onUploadProgress?: (p: UploadVersionProgress) => void;
}

type RawResponse = components['schemas']['UploadResponse'];

function toProgress(e: AxiosProgressEvent): UploadVersionProgress {
  const total = typeof e.total === 'number' && e.total > 0 ? e.total : undefined;
  const fraction = total !== undefined ? Math.min(1, e.loaded / total) : undefined;
  const result: UploadVersionProgress = { loaded: e.loaded };
  if (total !== undefined) result.total = total;
  if (fraction !== undefined) result.fraction = fraction;
  return result;
}

/**
 * Проверяет, что 202-ответ содержит обязательные поля. Backend по контракту
 * всегда возвращает их, но OpenAPI schema optional — защита от drift'а.
 */
function narrowResponse(raw: RawResponse): UploadVersionResponse {
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
  const result: UploadVersionResponse = {
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
 * Загружает новую версию существующего договора. Axios сам выставит
 * `multipart/form-data; boundary=...` если Content-Type не передан в config.
 */
export async function uploadVersion(
  input: UploadVersionInput,
  opts: UploadVersionOptions = {},
): Promise<UploadVersionResponse> {
  const fd = new FormData();
  fd.append('file', input.file);

  const http = getHttpInstance();
  const { data } = await http.post<RawResponse>(endpointFor(input.contractId), fd, {
    timeout: UPLOAD_TIMEOUT_MS,
    ...(opts.signal && { signal: opts.signal }),
    ...(opts.onUploadProgress && {
      onUploadProgress: (e: AxiosProgressEvent) => {
        opts.onUploadProgress?.(toProgress(e));
      },
    }),
  });

  return narrowResponse(data);
}

export { endpointFor as uploadVersionEndpoint };
