// Handlers сравнения версий: POST /contracts/{id}/compare + GET diff.

import { http, HttpResponse } from 'msw';

import * as diff from '../fixtures/diff';
import { IDS } from '../fixtures/ids';
import { errorResponse, type HandlerBase, joinPath } from './_helpers';

export function createComparisonHandlers(base: HandlerBase) {
  return [
    http.post(
      joinPath(base, '/contracts/:contractId/compare'),
      async ({ request }) => {
        const body = (await request.json().catch(() => null)) as
          | { base_version_id?: string; target_version_id?: string }
          | null;
        if (!body?.base_version_id || !body?.target_version_id) {
          return errorResponse(400, 'VALIDATION_ERROR', 'Проверьте введённые данные');
        }
        return HttpResponse.json(
          { job_id: IDS.jobs.comparison, status: 'QUEUED' },
          { status: 202 },
        );
      },
    ),

    http.get(
      joinPath(base, '/contracts/:contractId/versions/:baseVersionId/diff/:targetVersionId'),
      () => HttpResponse.json(diff.versionDiffAlpha, { status: 200 }),
    ),
  ];
}
