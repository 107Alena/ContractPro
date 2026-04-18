// GET /contracts/{contract_id}/versions/{version_id}/export/{format}
// (§7.6 high-architecture + api-specification).
//
// Контракт: 302 с заголовком Location на presigned URL (TTL 5 мин).
//   - Запрос идёт с `maxRedirects: 0` + `validateStatus: s => s === 302`,
//     чтобы axios не следовал редиректу автоматически, а вернул response
//     с доступным Location в headers.
//   - В тестовом окружении MSW-handler возвращает 302 с синтетическим
//     Location (`tests/msw/handlers/export.ts`) — axios fetch-adapter
//     пропускает Response как есть.
//   - В production Orchestrator должен добавлять `Access-Control-Expose-Headers:
//     Location` (same-origin gateway → CORS не активен; если когда-то станет
//     cross-origin — см. §18 открытые вопросы).
//
// 403/404 → OrchestratorError (нормализация interceptor'ом в shared/api).
import type { AxiosInstance } from 'axios';

import { OrchestratorError } from '@/shared/api';

import type { ExportLocation, ExportReportInput } from '../model/types';
import { getHttpInstance } from './http';

function endpointFor(input: ExportReportInput): string {
  return (
    `/contracts/${encodeURIComponent(input.contractId)}` +
    `/versions/${encodeURIComponent(input.versionId)}` +
    `/export/${encodeURIComponent(input.format)}`
  );
}

export interface ExportReportOptions {
  signal?: AbortSignal;
}

/**
 * Читает Location из axios-ответа, независимо от типа коллекции заголовков.
 * Axios может отдавать `headers` как plain object или как AxiosHeaders.
 */
function readLocation(headers: unknown): string | undefined {
  if (headers === null || headers === undefined) return undefined;
  // AxiosHeaders имеет метод get — пользуемся им, чтобы не читать приватные поля.
  if (typeof (headers as { get?: (k: string) => unknown }).get === 'function') {
    const v = (headers as { get: (k: string) => unknown }).get('location');
    return typeof v === 'string' ? v : undefined;
  }
  if (typeof headers === 'object') {
    const raw = (headers as Record<string, unknown>)['location'];
    return typeof raw === 'string' ? raw : undefined;
  }
  return undefined;
}

/**
 * Выполняет export-запрос и возвращает presigned URL.
 * Бросает OrchestratorError('INTERNAL_ERROR') если 302 пришло без Location —
 * защитный контракт-guard от backend-drift'а.
 */
export async function exportReport(
  input: ExportReportInput,
  opts: ExportReportOptions = {},
): Promise<ExportLocation> {
  const httpClient: AxiosInstance = getHttpInstance();
  const response = await httpClient.get(endpointFor(input), {
    // http-adapter (Node) — respects maxRedirects:0; fetch-adapter (Node/browser)
    // — ignores maxRedirects, нужно `fetchOptions.redirect: 'manual'`.
    maxRedirects: 0,
    fetchOptions: { redirect: 'manual' },
    validateStatus: (status) => status === 302,
    ...(opts.signal && { signal: opts.signal }),
  });
  const location = readLocation(response.headers);
  if (!location) {
    throw new OrchestratorError({
      error_code: 'INTERNAL_ERROR',
      message: 'Не удалось получить ссылку на отчёт. Повторите попытку.',
      status: 502,
    });
  }
  return { location };
}

export { endpointFor as exportReportEndpoint };
