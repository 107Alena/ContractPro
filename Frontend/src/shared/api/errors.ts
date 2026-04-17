// Нормализованная доменная ошибка HTTP-клиента (§7.2-7.3 high-architecture).
// Источник контракта — `ErrorResponse` из openapi.d.ts:
//   { error_code, message, details?, suggestion?, correlation_id? }
//
// FE-TASK-014 развернёт `shared/api/errors/` как каталог (codes, catalog, handler,
// apply-validation) — при переезде этот файл станет `errors/orchestrator-error.ts`
// с ре-экспортом из barrel без breaking changes для потребителей.
import type { components } from '@/shared/api/openapi';

export type ErrorResponse = components['schemas']['ErrorResponse'];
export type ErrorDetails = ErrorResponse['details'];

export interface OrchestratorErrorOptions {
  error_code: string;
  message: string;
  suggestion?: string | null;
  details?: ErrorDetails;
  correlationId?: string;
  status?: number;
  cause?: unknown;
}

export class OrchestratorError extends Error {
  readonly error_code: string;
  readonly suggestion?: string | null;
  readonly details?: ErrorDetails;
  readonly correlationId?: string;
  readonly status?: number;

  constructor(opts: OrchestratorErrorOptions) {
    super(opts.message, opts.cause !== undefined ? { cause: opts.cause } : undefined);
    this.name = 'OrchestratorError';
    this.error_code = opts.error_code;
    if (opts.suggestion !== undefined) this.suggestion = opts.suggestion;
    if (opts.details !== undefined) this.details = opts.details;
    if (opts.correlationId !== undefined) this.correlationId = opts.correlationId;
    if (opts.status !== undefined) this.status = opts.status;
  }
}

// Sentinel-коды для случаев без ErrorResponse от backend (network/timeout/abort/неожиданный non-JSON).
// Совместимы с §7.3 семантикой: `error_code` — всегда строка, `message` — на русском (NFR-5.2).
// FE-TASK-014 внесёт их в ERROR_UX каталог.
export const CLIENT_ERROR_CODES = {
  NETWORK_ERROR: 'NETWORK_ERROR',
  TIMEOUT: 'TIMEOUT',
  REQUEST_ABORTED: 'REQUEST_ABORTED',
  UNKNOWN_ERROR: 'UNKNOWN_ERROR',
} as const;
