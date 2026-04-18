// GET /contracts/{contract_id}/versions/{version_id}/export/{format} — тот же
// endpoint, что используется в feature `export-download`. Feature share-link
// дублирует axios-обёртку, поскольку FSD-границы запрещают импорт между
// слайсами features/* (см. eslint.config `boundaries/element-types`).
//
// Отличие от export-download: результат не ведёт к window.location.assign,
// а копируется пользователем в clipboard (§7.6 + ТЗ UR-10: «получать
// защищённую ссылку для передачи не-авторизованным»).
import type { AxiosInstance } from 'axios';

import { OrchestratorError } from '@/shared/api';

import type { ShareLinkInput, ShareLinkResult } from '../model/types';
import { getHttpInstance } from './http';

function endpointFor(input: ShareLinkInput): string {
  return (
    `/contracts/${encodeURIComponent(input.contractId)}` +
    `/versions/${encodeURIComponent(input.versionId)}` +
    `/export/${encodeURIComponent(input.format)}`
  );
}

export interface GetShareLinkOptions {
  signal?: AbortSignal;
}

function readLocation(headers: unknown): string | undefined {
  if (headers === null || headers === undefined) return undefined;
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
 * Запрашивает защищённую ссылку на отчёт. Бросает OrchestratorError, если
 * 302 пришло без Location — защита от backend-drift'а.
 */
export async function getShareLink(
  input: ShareLinkInput,
  opts: GetShareLinkOptions = {},
): Promise<ShareLinkResult> {
  const httpClient: AxiosInstance = getHttpInstance();
  const response = await httpClient.get(endpointFor(input), {
    // http-adapter (Node) — respects maxRedirects:0; fetch-adapter — ignores,
    // нужно `fetchOptions.redirect: 'manual'` для 302-с-Location (см. §7.6).
    maxRedirects: 0,
    fetchOptions: { redirect: 'manual' },
    validateStatus: (status) => status === 302,
    ...(opts.signal && { signal: opts.signal }),
  });
  const location = readLocation(response.headers);
  if (!location) {
    throw new OrchestratorError({
      error_code: 'INTERNAL_ERROR',
      message: 'Не удалось получить ссылку для отправки. Повторите попытку.',
      status: 502,
    });
  }
  return { location };
}

export { endpointFor as shareLinkEndpoint };
