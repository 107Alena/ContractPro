// Доменные типы feature contract-upload.
//
// `UploadContractResponse` — narrowed-non-null версия OpenAPI-типа
// `UploadResponse`: OpenAPI-контракт типизирует все поля как optional, но
// спецификация §7.5 и §16.2 гарантируют их наличие в 202-ответе. Narrow
// делается на границе transport (upload-contract.ts) с runtime-guard.
import type { components } from '@/shared/api/openapi';

type UploadResponseRaw = components['schemas']['UploadResponse'];
export type UserProcessingStatus = components['schemas']['UserProcessingStatus'];

export interface UploadContractInput {
  file: File;
  title: string;
}

export interface UploadContractResponse {
  contractId: string;
  versionId: string;
  versionNumber: number;
  jobId: string;
  status: UserProcessingStatus;
  message?: string;
}

export interface UploadProgress {
  loaded: number;
  total?: number;
  /** Доля от 0 до 1. Undefined, если размер неизвестен. */
  fraction?: number;
}

/**
 * Имена полей формы, на которые хук проставляет inline-ошибки через
 * `setError`. Должны совпадать с именами FieldDropZone-wrapper'а и
 * title-input'а на странице-потребителе.
 */
export const UPLOAD_FORM_FIELDS = {
  file: 'file',
  title: 'title',
} as const;

/**
 * Дефолтный shape формы загрузки. `file: File | null` соответствует реальному
 * значению поля (до первого выбора — null). Используется как generic-default
 * для `UseUploadContractOptions<TForm>`, но потребители часто переопределяют
 * на свой rhf-тип с дополнительными полями.
 */
export type UploadFormValues = {
  file: File | null;
  title: string;
};

/** @internal — type-level smoke-test совместимости с OpenAPI. */
export type __UploadResponseRaw = UploadResponseRaw;
