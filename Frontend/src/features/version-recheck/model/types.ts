// Доменные типы feature version-recheck.
//
// Endpoint: POST /contracts/{contract_id}/versions/{version_id}/recheck.
// Запрос без тела; ответ 202 — тот же `UploadResponse` (новая версия с
// origin_type=RE_CHECK). Narrow non-null через runtime-guard.
import type { components } from '@/shared/api/openapi';

type UploadResponseRaw = components['schemas']['UploadResponse'];
export type UserProcessingStatus = components['schemas']['UserProcessingStatus'];

export interface RecheckVersionInput {
  contractId: string;
  versionId: string;
}

export interface RecheckVersionResponse {
  contractId: string;
  versionId: string;
  versionNumber: number;
  jobId: string;
  status: UserProcessingStatus;
  message?: string;
}

/** @internal — type-level smoke-test совместимости с OpenAPI. */
export type __UploadResponseRaw = UploadResponseRaw;
