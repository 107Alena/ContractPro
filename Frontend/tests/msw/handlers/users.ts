// Handler GET /users/me.
// По умолчанию возвращает LAWYER. Тест/story может переопределить через:
//
//  1) Статическую фабрику:
//     server.use(...createUsersHandlers(base, { me: fixtures.users.businessUser })).
//
//  2) Рантайм-override через `setE2EUserRole(role)` — хранится в модульном
//     state и читается handler'ом на каждом запросе. Используется в Playwright:
//     addInitScript кладёт window.__cpE2eRole__, browser.ts при старте worker'а
//     вызывает setE2EUserRole.

import { http, HttpResponse } from 'msw';

import type { components } from '@/shared/api/openapi';

import * as users from '../fixtures/users';
import { type HandlerBase, joinPath } from './_helpers';

type UserProfile = components['schemas']['UserProfile'];
type UserRole = UserProfile['role'];

const ROLE_TO_FIXTURE: Record<UserRole, UserProfile> = {
  LAWYER: users.lawyer,
  BUSINESS_USER: users.businessUser,
  ORG_ADMIN: users.orgAdmin,
};

let runtimeRoleOverride: UserRole | null = null;

/**
 * Runtime-override роли для GET /users/me. Живёт в browser-worker'е
 * (tests/msw/browser.ts): до worker.start() читаем `window.__cpE2eRole__`
 * и вызываем setE2EUserRole. Сбрасывается setE2EUserRole(null).
 *
 * Граф tests/msw/* попадает в бандл только под
 * `import.meta.env.DEV && VITE_ENABLE_MSW === 'true'` (main.tsx) — rollup
 * tree-shake выкусывает эту функцию вместе с handler'ами в prod-бандле.
 */
export function setE2EUserRole(role: UserRole | null): void {
  runtimeRoleOverride = role;
}

export function createUsersHandlers(base: HandlerBase, overrides: { me?: UserProfile } = {}) {
  const fallback = overrides.me ?? users.lawyer;
  return [
    http.get(joinPath(base, '/users/me'), () => {
      const me = runtimeRoleOverride ? ROLE_TO_FIXTURE[runtimeRoleOverride] : fallback;
      return HttpResponse.json(me, { status: 200 });
    }),
  ];
}
