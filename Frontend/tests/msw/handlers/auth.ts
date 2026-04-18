// Handlers /auth/login, /auth/refresh, /auth/logout.
// §17.1 endpoints. Default-поведение — happy-path; тесты переопределяют через
// server.use() для негативных сценариев (401/400/VALIDATION_ERROR).

import { http, HttpResponse } from 'msw';

import * as auth from '../fixtures/auth';
import { errorResponse, type HandlerBase, joinPath } from './_helpers';

export function createAuthHandlers(base: HandlerBase) {
  return [
    http.post(joinPath(base, '/auth/login'), async ({ request }) => {
      const body = (await request.json().catch(() => null)) as
        | { email?: string; password?: string }
        | null;
      if (!body?.email || !body?.password) {
        return errorResponse(400, 'VALIDATION_ERROR', 'Проверьте введённые данные', {
          details: {
            fields: [
              { field: 'email', code: 'REQUIRED', message: 'Email обязателен для заполнения.' },
            ],
          } as never,
        });
      }
      return HttpResponse.json(auth.validTokens, { status: 200 });
    }),

    http.post(joinPath(base, '/auth/refresh'), async ({ request }) => {
      const body = (await request.json().catch(() => null)) as { refresh_token?: string } | null;
      if (!body?.refresh_token) {
        return errorResponse(401, 'AUTH_TOKEN_INVALID', 'Невалидная авторизация. Войдите заново.');
      }
      return HttpResponse.json(auth.validTokens, { status: 200 });
    }),

    http.post(joinPath(base, '/auth/logout'), () => new HttpResponse(null, { status: 204 })),
  ];
}
