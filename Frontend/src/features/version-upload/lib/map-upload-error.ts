// Маппит upload-специфичные доменные коды на inline-field-ошибки (§9.3 row 413/415).
//
// 413 FILE_TOO_LARGE / 415 UNSUPPORTED_FORMAT / 400 INVALID_FILE — три кода,
// семантически относящиеся к полю `file` в форме загрузки версии. Общий
// whitelist кодов живёт в `@/shared/api` (FILE_FIELD_ERROR_CODES), чтобы
// contract-upload и version-upload не разошлись при добавлении нового кода.
import {
  type ErrorCode,
  FILE_FIELD_ERROR_CODES,
  isOrchestratorError,
  type OrchestratorError,
} from '@/shared/api';

import { UPLOAD_VERSION_FORM_FIELDS } from '../model/types';

export type UploadVersionFieldName =
  (typeof UPLOAD_VERSION_FORM_FIELDS)[keyof typeof UPLOAD_VERSION_FORM_FIELDS];

export interface UploadVersionFieldError {
  field: UploadVersionFieldName;
  code: ErrorCode;
  message: string;
}

const FILE_FIELD_CODES: readonly ErrorCode[] = FILE_FIELD_ERROR_CODES;

/**
 * Если `err` — OrchestratorError с кодом из whitelist — возвращает структуру
 * для `setError`. Иначе null (пусть общий error-handler показывает toast).
 */
export function mapUploadVersionError(err: unknown): UploadVersionFieldError | null {
  if (!isOrchestratorError(err)) return null;
  if (!FILE_FIELD_CODES.includes(err.error_code as ErrorCode)) return null;
  const msg = err.message && err.message.trim() !== '' ? err.message : err.error_code;
  return {
    field: UPLOAD_VERSION_FORM_FIELDS.file,
    code: err.error_code as ErrorCode,
    message: msg,
  };
}

/**
 * Type-guard для кодов, которые относятся к полю `file` и должны быть
 * проставлены inline, а не через toast.
 */
export function isUploadVersionFileFieldError(err: unknown): err is OrchestratorError {
  return isOrchestratorError(err) && FILE_FIELD_CODES.includes(err.error_code as ErrorCode);
}
