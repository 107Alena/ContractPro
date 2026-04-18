// Handler POST /contracts/{id}/versions/{vid}/feedback.

import { http, HttpResponse } from 'msw';

import { IDS } from '../fixtures/ids';
import { errorResponse, type HandlerBase, joinPath } from './_helpers';

export function createFeedbackHandlers(base: HandlerBase) {
  return [
    http.post(
      joinPath(base, '/contracts/:contractId/versions/:versionId/feedback'),
      async ({ request }) => {
        const body = (await request.json().catch(() => null)) as
          | { is_useful?: boolean; comment?: string }
          | null;
        if (body === null || typeof body.is_useful !== 'boolean') {
          return errorResponse(400, 'VALIDATION_ERROR', 'Проверьте введённые данные');
        }
        return HttpResponse.json(
          { feedback_id: IDS.feedback, created_at: '2026-04-18T10:00:00Z' },
          { status: 201 },
        );
      },
    ),
  ];
}
