// Доменные типы feature version-upload.
//
// Endpoint: POST /contracts/{contract_id}/versions/upload (§7.5 api-specification).
// Ответ 202 — тот же `UploadResponse`, что и для первичной загрузки: backend
// переиспользует схему. Narrow non-null поля через runtime-guard на границе
// transport (upload-version.ts).
import type { components } from '@/shared/api/openapi';

type UploadResponseRaw = components['schemas']['UploadResponse'];
export type UserProcessingStatus = components['schemas']['UserProcessingStatus'];

export interface UploadVersionInput {
  contractId: string;
  file: File;
}

export interface UploadVersionResponse {
  contractId: string;
  versionId: string;
  versionNumber: number;
  jobId: string;
  status: UserProcessingStatus;
  message?: string;
}

export interface UploadVersionProgress {
  loaded: number;
  total?: number;
  /** Доля от 0 до 1. Undefined, если размер неизвестен. */
  fraction?: number;
}

/**
 * Единственное поле формы, на которое проставляются inline-ошибки.
 * В отличие от первичной загрузки, title не передаётся (берётся от уже
 * существующего договора).
 */
export const UPLOAD_VERSION_FORM_FIELDS = {
  file: 'file',
} as const;

/**
 * Дефолтный shape формы загрузки новой версии. Потребитель может расширить
 * через generic `TForm`, если добавляет доп. поля (например, note).
 */
export type UploadVersionFormValues = {
  file: File | null;
};

/** @internal — type-level smoke-test совместимости с OpenAPI. */
export type __UploadResponseRaw = UploadResponseRaw;
