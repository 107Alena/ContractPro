// Handlers /contracts/*: upload, list, get, delete, archive.
// Базовые happy-path ответы; негативные сценарии — через server.use() в тестах.

import { http, HttpResponse } from 'msw';

import type { components } from '@/shared/api/openapi';

import * as contracts from '../fixtures/contracts';
import { IDS } from '../fixtures/ids';
import { errorResponse, type HandlerBase, joinPath } from './_helpers';

type ContractSummary = components['schemas']['ContractSummary'];

const RISK_RANK: Record<string, number> = { high: 3, medium: 2, low: 1 };

// Воспроизводит server-side фильтры/сортировку GET /contracts (ORCH-TASK-056),
// чтобы dev:e2e и e2e показывали реальную работу чипов/сортировки. Массивы —
// через getAll (repeated keys, как сериализует фронт).
function applyContractsQuery(
  all: readonly ContractSummary[],
  sp: URLSearchParams,
): ContractSummary[] {
  let items = [...all];

  const status = sp.get('status');
  if (status) items = items.filter((c) => c.status === status);

  const search = sp.get('search');
  if (search) {
    const q = search.toLowerCase();
    items = items.filter((c) => (c.title ?? '').toLowerCase().includes(q));
  }

  const risk = sp.get('risk_level');
  if (risk) items = items.filter((c) => c.risk_level === risk);

  const types = sp.getAll('contract_type');
  if (types.length > 0)
    items = items.filter((c) => c.contract_type != null && types.includes(c.contract_type));

  const statuses = sp.getAll('processing_status');
  if (statuses.length > 0)
    items = items.filter(
      (c) => c.processing_status != null && statuses.includes(c.processing_status),
    );

  const dateFrom = sp.get('date_from');
  if (dateFrom) items = items.filter((c) => (c.created_at ?? '') >= dateFrom);
  const dateTo = sp.get('date_to');
  if (dateTo) items = items.filter((c) => (c.created_at ?? '') <= `${dateTo}T23:59:59Z`);

  const sort = sp.get('sort');
  if (sort) {
    const dir = sp.get('order') === 'asc' ? 1 : -1;
    items.sort((a, b) => {
      if (sort === 'title') return (a.title ?? '').localeCompare(b.title ?? '', 'ru') * dir;
      if (sort === 'risk') {
        const ra = a.risk_level ? (RISK_RANK[a.risk_level] ?? 0) : 0;
        const rb = b.risk_level ? (RISK_RANK[b.risk_level] ?? 0) : 0;
        if (ra === 0 || rb === 0) return ra === rb ? 0 : ra === 0 ? 1 : -1; // null всегда в конец
        return (ra - rb) * dir;
      }
      // date (created_at)
      const da = a.created_at ?? '';
      const db = b.created_at ?? '';
      return (da < db ? -1 : da > db ? 1 : 0) * dir;
    });
  }

  return items;
}

export function createContractsHandlers(base: HandlerBase) {
  return [
    // POST /contracts/upload — multipart, возвращает UploadResponse.
    http.post(joinPath(base, '/contracts/upload'), () =>
      HttpResponse.json(
        {
          contract_id: IDS.contracts.alpha,
          version_id: IDS.versions.alphaV1,
          version_number: 1,
          job_id: IDS.jobs.processing,
          status: 'QUEUED',
          message: 'В очереди на обработку',
        },
        { status: 202 },
      ),
    ),

    // GET /contracts — список (server-side filter + sort + pagination).
    http.get(joinPath(base, '/contracts'), ({ request }) => {
      const url = new URL(request.url);
      const page = Number(url.searchParams.get('page') ?? 1);
      const size = Number(url.searchParams.get('size') ?? 20);
      const filtered = applyContractsQuery(contracts.contractSummaries, url.searchParams);
      const start = (page - 1) * size;
      const items = filtered.slice(start, start + size);
      return HttpResponse.json({ items, total: filtered.length, page, size }, { status: 200 });
    }),

    // GET /contracts/stats — агрегированная статистика (ДО :contractId, иначе
    // matcher примет stats за contractId).
    http.get(joinPath(base, '/contracts/stats'), () =>
      HttpResponse.json(contracts.contractStats, { status: 200 }),
    ),

    // GET /contracts/{id} — детали договора.
    http.get(joinPath(base, '/contracts/:contractId'), ({ params }) => {
      const contract = contracts.contractDetailsById[params.contractId as string];
      if (!contract) {
        return errorResponse(404, 'DOCUMENT_NOT_FOUND', 'Документ не найден');
      }
      return HttpResponse.json(contract, { status: 200 });
    }),

    // DELETE /contracts/{id} — 204.
    http.delete(
      joinPath(base, '/contracts/:contractId'),
      () => new HttpResponse(null, { status: 204 }),
    ),

    // POST /contracts/{id}/archive — 204.
    http.post(
      joinPath(base, '/contracts/:contractId/archive'),
      () => new HttpResponse(null, { status: 204 }),
    ),
  ];
}
