// Handlers для версий: list / upload / get / status / recheck / confirm-type.

import { http, HttpResponse } from 'msw';

import * as contracts from '../fixtures/contracts';
import { IDS } from '../fixtures/ids';
import { errorResponse, type HandlerBase, joinPath } from './_helpers';

export function createVersionsHandlers(base: HandlerBase) {
  return [
    // GET /contracts/{id}/versions
    http.get(joinPath(base, '/contracts/:contractId/versions'), ({ params }) => {
      const items = contracts.versionsByContract[params.contractId as string];
      if (!items) {
        return errorResponse(404, 'DOCUMENT_NOT_FOUND', 'Документ не найден');
      }
      return HttpResponse.json(
        { items, total: items.length, page: 1, size: items.length },
        { status: 200 },
      );
    }),

    // POST /contracts/{id}/versions/upload — multipart
    http.post(joinPath(base, '/contracts/:contractId/versions/upload'), ({ params }) =>
      HttpResponse.json(
        {
          contract_id: params.contractId as string,
          version_id: IDS.versions.alphaV2,
          version_number: 2,
          job_id: IDS.jobs.processing,
          status: 'QUEUED',
          message: 'В очереди на обработку',
        },
        { status: 202 },
      ),
    ),

    // GET /contracts/{id}/versions/{vid}
    http.get(
      joinPath(base, '/contracts/:contractId/versions/:versionId'),
      ({ params }) => {
        const versions = contracts.versionsByContract[params.contractId as string] ?? [];
        const version = versions.find((v) => v.version_id === params.versionId);
        if (!version) {
          return errorResponse(404, 'VERSION_NOT_FOUND', 'Версия не найдена');
        }
        return HttpResponse.json(version, { status: 200 });
      },
    ),

    // GET /contracts/{id}/versions/{vid}/status
    http.get(
      joinPath(base, '/contracts/:contractId/versions/:versionId/status'),
      ({ params }) => {
        const versions = contracts.versionsByContract[params.contractId as string] ?? [];
        const version = versions.find((v) => v.version_id === params.versionId);
        if (!version) {
          return errorResponse(404, 'VERSION_NOT_FOUND', 'Версия не найдена');
        }
        return HttpResponse.json(
          {
            version_id: version.version_id,
            status: version.processing_status,
            message: version.processing_status_message,
            updated_at: version.created_at,
          },
          { status: 200 },
        );
      },
    ),

    // POST /contracts/{id}/versions/{vid}/recheck
    http.post(
      joinPath(base, '/contracts/:contractId/versions/:versionId/recheck'),
      ({ params }) =>
        HttpResponse.json(
          {
            contract_id: params.contractId as string,
            version_id: params.versionId as string,
            version_number: 1,
            job_id: IDS.jobs.processing,
            status: 'QUEUED',
            message: 'В очереди на повторную проверку',
          },
          { status: 202 },
        ),
    ),

    // POST /contracts/{id}/versions/{vid}/confirm-type
    http.post(
      joinPath(base, '/contracts/:contractId/versions/:versionId/confirm-type'),
      async ({ params, request }) => {
        const body = (await request.json().catch(() => null)) as
          | { contract_type?: string; confirmed_by_user?: boolean }
          | null;
        if (!body?.contract_type || body.confirmed_by_user !== true) {
          return errorResponse(400, 'VALIDATION_ERROR', 'Проверьте введённые данные');
        }
        return HttpResponse.json(
          {
            contract_id: params.contractId as string,
            version_id: params.versionId as string,
            version_number: 1,
            job_id: IDS.jobs.processing,
            status: 'ANALYZING',
            message: 'Юридический анализ',
          },
          { status: 202 },
        );
      },
    ),
  ];
}
