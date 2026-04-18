// Handler GET /users/me.
// Возвращает LAWYER по умолчанию; тест/story может переопределить
// через server.use(createUsersHandlers(base, {me: fixtures.users.businessUser})).

import { http, HttpResponse } from 'msw';

import type { components } from '@/shared/api/openapi';

import * as users from '../fixtures/users';
import { type HandlerBase, joinPath } from './_helpers';

type UserProfile = components['schemas']['UserProfile'];

export function createUsersHandlers(
  base: HandlerBase,
  overrides: { me?: UserProfile } = {},
) {
  const me = overrides.me ?? users.lawyer;
  return [http.get(joinPath(base, '/users/me'), () => HttpResponse.json(me, { status: 200 }))];
}
