// Реестр кодов ошибок Orchestrator API (§7.3 high-architecture).
//
// Источник правды — backend-контракт (`ErrorResponse.error_code`). Тип в
// openapi.d.ts объявлен как `string` (open set), но фронту важно иметь
// закрытый union для полного mapping'а ERROR_UX и проверки компилятором
// «все 22 кода покрыты».
//
// Плюс — клиентские sentinel-коды (§7.2 network/timeout/abort/unknown):
// возникают ДО получения ответа от сервера и, следовательно, отсутствуют
// в OpenAPI-схеме. Структурно совместимы (`error_code: string`).

/** 22 серверных кода по §7.3. Порядок совпадает с ERROR_UX для удобства review. */
export const SERVER_ERROR_CODES = [
  'AUTH_TOKEN_MISSING',
  'AUTH_TOKEN_EXPIRED',
  'AUTH_TOKEN_INVALID',
  'PERMISSION_DENIED',
  'FILE_TOO_LARGE',
  'UNSUPPORTED_FORMAT',
  'INVALID_FILE',
  'DOCUMENT_NOT_FOUND',
  'VERSION_NOT_FOUND',
  'ARTIFACT_NOT_FOUND',
  'DIFF_NOT_FOUND',
  'DOCUMENT_ARCHIVED',
  'DOCUMENT_DELETED',
  'VERSION_STILL_PROCESSING',
  'RESULTS_NOT_READY',
  'RATE_LIMIT_EXCEEDED',
  'STORAGE_UNAVAILABLE',
  'DM_UNAVAILABLE',
  'OPM_UNAVAILABLE',
  'BROKER_UNAVAILABLE',
  'VALIDATION_ERROR',
  'INTERNAL_ERROR',
] as const;

export type ServerErrorCode = (typeof SERVER_ERROR_CODES)[number];

/** Клиентские sentinel-коды (нет ответа от сервера — network/timeout/abort/unknown). */
export const CLIENT_ERROR_CODES = {
  NETWORK_ERROR: 'NETWORK_ERROR',
  TIMEOUT: 'TIMEOUT',
  REQUEST_ABORTED: 'REQUEST_ABORTED',
  UNKNOWN_ERROR: 'UNKNOWN_ERROR',
} as const;

export type ClientErrorCode = (typeof CLIENT_ERROR_CODES)[keyof typeof CLIENT_ERROR_CODES];

/** Полный union кодов — ключи ERROR_UX. Любой новый код должен появиться здесь сначала. */
export type ErrorCode = ServerErrorCode | ClientErrorCode;

/** UX-действие, которое UI может предложить пользователю по коду ошибки. */
export type ErrorAction = 'retry' | 'login' | 'none';

// Inline-const для type-guard'а без ре-allocation на каждом вызове.
const KNOWN_ERROR_CODES = new Set<string>([
  ...SERVER_ERROR_CODES,
  ...(Object.values(CLIENT_ERROR_CODES) as string[]),
]);

/** Проверяет, что `error_code` из произвольного ErrorResponse относится к закрытому union'у. */
export function isKnownErrorCode(code: string): code is ErrorCode {
  return KNOWN_ERROR_CODES.has(code);
}
