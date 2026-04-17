// Нормализованная доменная ошибка HTTP-клиента (§7.2-7.3 high-architecture).
// Источник контракта — `ErrorResponse` из openapi.d.ts:
//   { error_code, message, details?, suggestion?, correlation_id? }
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

  /**
   * camelCase-алиас для `error_code`. Введён для соответствия §20.4 архитектуры
   * (`err.code`) без breaking-change существующих потребителей (client.ts,
   * тесты). Read-only getter: сериализация и equality-проверки поведения не меняются.
   */
  get code(): string {
    return this.error_code;
  }
}

/** Type-guard: полезен в error-handler'ах и MSW-тестах. */
export function isOrchestratorError(err: unknown): err is OrchestratorError {
  return err instanceof OrchestratorError;
}
