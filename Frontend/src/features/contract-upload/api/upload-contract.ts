// POST /contracts/upload (§7.5, §16.2 high-architecture).
//
// Контракт: multipart/form-data { file, title } → 202 UploadResponse.
// Тонкая обёртка: собирает FormData, вызывает axios, сужает тип ответа.
// Всю логику маппинга ошибок (413/415/INVALID_FILE → inline-field) выполняет
// `useUploadContract` — api-слой прокидывает `OrchestratorError` как есть,
// interceptor в shared/api/client уже нормализовал тело.
import type { AxiosProgressEvent } from 'axios';

import type { components } from '@/shared/api/openapi';

import type {
  UploadContractInput,
  UploadContractResponse,
  UploadProgress,
} from '../model/types';
import { getHttpInstance } from './http';

const ENDPOINT = '/contracts/upload';
/**
 * Увеличенный таймаут для upload: дефолтный 30s мал для PDF до 20 МБ при
 * медленном канале. Формула §7.5: 120s.
 */
const UPLOAD_TIMEOUT_MS = 120_000;

export interface UploadContractOptions {
  signal?: AbortSignal;
  onUploadProgress?: (p: UploadProgress) => void;
}

type RawResponse = components['schemas']['UploadResponse'];

/**
 * Приводит axios-progress-event к доменному `UploadProgress`. Axios не гарантирует
 * `total` (Content-Length может отсутствовать) — `fraction` рассчитываем только
 * когда total известен и > 0.
 */
function toProgress(e: AxiosProgressEvent): UploadProgress {
  const total = typeof e.total === 'number' && e.total > 0 ? e.total : undefined;
  const fraction = total !== undefined ? Math.min(1, e.loaded / total) : undefined;
  const result: UploadProgress = { loaded: e.loaded };
  if (total !== undefined) result.total = total;
  if (fraction !== undefined) result.fraction = fraction;
  return result;
}

/**
 * Проверяет, что 202-ответ содержит обязательные поля. Бэкенд по контракту
 * всегда возвращает их в 202, но OpenAPI schema их optional — защита от
 * спецификационного drift'а.
 */
function narrowResponse(raw: RawResponse): UploadContractResponse {
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
  const result: UploadContractResponse = {
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
 * Загружает файл договора. Axios сам выставит `multipart/form-data; boundary=...`
 * если Content-Type не передан в config — в текущем `createHttpClient` он не
 * захардкожен (проверено тестом). Не передаём Content-Type явно, чтобы не
 * сломать boundary.
 */
export async function uploadContract(
  input: UploadContractInput,
  opts: UploadContractOptions = {},
): Promise<UploadContractResponse> {
  const fd = new FormData();
  fd.append('file', input.file);
  fd.append('title', input.title);

  const http = getHttpInstance();
  const { data } = await http.post<RawResponse>(ENDPOINT, fd, {
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

export { ENDPOINT as UPLOAD_CONTRACT_ENDPOINT };
