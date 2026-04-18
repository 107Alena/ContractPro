// Общие хелперы для handlers: построение URL с учётом baseURL, типовые
// ErrorResponse-конструкторы и корреляционный id.
// Base URL захватывается абсолютным префиксом (e.g. 'http://localhost/api/v1'),
// чтобы относительные паттерны MSW v2 не матчили другие origin'ы (§10.3).

import { HttpResponse } from 'msw';

import type { components } from '@/shared/api/openapi';

type ErrorResponse = components['schemas']['ErrorResponse'];

export type HandlerBase = string;

export function joinPath(base: HandlerBase, path: string): string {
  if (!path.startsWith('/')) throw new Error(`path must start with '/': ${path}`);
  return `${base}${path}`;
}

/** Детерминированный correlation-id для фикстур. */
export const MOCK_CORRELATION_ID = '00000000-0000-0000-0000-00000000c0c0';

export function errorResponse(
  status: number,
  errorCode: string,
  message: string,
  extra: Partial<Pick<ErrorResponse, 'suggestion' | 'details'>> = {},
): ReturnType<typeof HttpResponse.json<ErrorResponse>> {
  const body: ErrorResponse = {
    error_code: errorCode,
    message,
    correlation_id: MOCK_CORRELATION_ID,
    ...(extra.suggestion !== undefined && { suggestion: extra.suggestion }),
    ...(extra.details !== undefined && { details: extra.details }),
  };
  return HttpResponse.json(body, { status });
}
