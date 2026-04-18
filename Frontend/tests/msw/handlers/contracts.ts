// Handlers /contracts/*: upload, list, get, delete, archive.
// Базовые happy-path ответы; негативные сценарии — через server.use() в тестах.

import { http, HttpResponse } from 'msw';

import * as contracts from '../fixtures/contracts';
import { IDS } from '../fixtures/ids';
import { errorResponse, type HandlerBase, joinPath } from './_helpers';

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

    // GET /contracts — список (server-side pagination).
    http.get(joinPath(base, '/contracts'), ({ request }) => {
      const url = new URL(request.url);
      const page = Number(url.searchParams.get('page') ?? 1);
      const size = Number(url.searchParams.get('size') ?? 20);
      const items = contracts.contractSummaries;
      return HttpResponse.json({ items, total: items.length, page, size }, { status: 200 });
    }),

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
