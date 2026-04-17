// Маппит upload-специфичные доменные коды на inline-field-ошибки (§9.3 row 413/415).
//
// 413 FILE_TOO_LARGE / 415 UNSUPPORTED_FORMAT / 400 INVALID_FILE — все три
// семантически относятся к полю `file` в форме загрузки. Они не приходят как
// VALIDATION_ERROR с `details.fields`, поэтому стандартный
// `applyValidationErrors` их не распознает. Этот хелпер решает какой код
// положить на какое поле — и возвращает null для прочих ошибок, которые
// должны обрабатываться как toast (500) или прокидываться (401).
import { type ErrorCode, isOrchestratorError, type OrchestratorError } from '@/shared/api';

import { UPLOAD_FORM_FIELDS } from '../model/types';

export type UploadFieldName = (typeof UPLOAD_FORM_FIELDS)[keyof typeof UPLOAD_FORM_FIELDS];

export interface UploadFieldError {
  field: UploadFieldName;
  code: ErrorCode;
  message: string;
}

const FILE_FIELD_CODES: readonly ErrorCode[] = [
  'FILE_TOO_LARGE',
  'UNSUPPORTED_FORMAT',
  'INVALID_FILE',
];

/**
 * Если `err` — OrchestratorError с кодом из whitelist (FILE_TOO_LARGE /
 * UNSUPPORTED_FORMAT / INVALID_FILE) — возвращает структуру для `setError`.
 * Иначе null (пусть общий error-handler показывает toast).
 *
 * Invariant: VALIDATION_ERROR не обрабатывается — для него есть
 * `applyValidationErrors`. Хук вызывает оба маппера последовательно.
 */
export function mapUploadFileError(err: unknown): UploadFieldError | null {
  if (!isOrchestratorError(err)) return null;
  if (!FILE_FIELD_CODES.includes(err.error_code as ErrorCode)) return null;
  const msg = err.message && err.message.trim() !== '' ? err.message : err.error_code;
  return {
    field: UPLOAD_FORM_FIELDS.file,
    code: err.error_code as ErrorCode,
    message: msg,
  };
}

/**
 * Type-guard для кодов, которые относятся к полю `file` и должны быть
 * проставлены inline, а не через toast.
 */
export function isFileFieldError(err: unknown): err is OrchestratorError {
  return isOrchestratorError(err) && FILE_FIELD_CODES.includes(err.error_code as ErrorCode);
}
