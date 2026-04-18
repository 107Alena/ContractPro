// Admin handlers: GET /admin/policies, PUT /admin/policies/{id},
// GET /admin/checklists, PUT /admin/checklists/{id}.

import { http, HttpResponse } from 'msw';

import * as admin from '../fixtures/admin';
import { errorResponse, type HandlerBase, joinPath } from './_helpers';

export function createAdminHandlers(base: HandlerBase) {
  return [
    http.get(joinPath(base, '/admin/policies'), () =>
      HttpResponse.json({ items: admin.policies }, { status: 200 }),
    ),

    http.put(joinPath(base, '/admin/policies/:policyId'), async ({ params, request }) => {
      const body = (await request.json().catch(() => null)) as
        | { settings?: Record<string, unknown> }
        | null;
      if (!body) {
        return errorResponse(400, 'VALIDATION_ERROR', 'Проверьте введённые данные');
      }
      const policy = admin.policies.find((p) => p.policy_id === params.policyId);
      if (!policy) {
        return errorResponse(404, 'DOCUMENT_NOT_FOUND', 'Политика не найдена');
      }
      return HttpResponse.json(
        { ...policy, settings: body.settings ?? policy.settings },
        { status: 200 },
      );
    }),

    http.get(joinPath(base, '/admin/checklists'), () =>
      HttpResponse.json({ items: admin.checklists }, { status: 200 }),
    ),

    http.put(
      joinPath(base, '/admin/checklists/:checklistId'),
      async ({ params, request }) => {
        const body = (await request.json().catch(() => null)) as
          | {
              items?: {
                id?: string;
                enabled?: boolean;
                severity?: 'high' | 'medium' | 'low';
              }[];
            }
          | null;
        if (!body) {
          return errorResponse(400, 'VALIDATION_ERROR', 'Проверьте введённые данные');
        }
        const checklist = admin.checklists.find((c) => c.checklist_id === params.checklistId);
        if (!checklist) {
          return errorResponse(404, 'DOCUMENT_NOT_FOUND', 'Чек-лист не найден');
        }
        return HttpResponse.json(checklist, { status: 200 });
      },
    ),
  ];
}
