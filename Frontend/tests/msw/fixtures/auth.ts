// Фикстуры auth-токенов. TTL-значения совпадают с бекенд-конфигом
// (access 15 мин, refresh 30 дней — см. configuration.md).

import type { components } from '@/shared/api/openapi';

type AuthTokens = components['schemas']['AuthTokens'];

export const validTokens: AuthTokens = {
  access_token: 'eyJhbGciOiJSUzI1NiJ9.mock-access-token.signature',
  refresh_token: 'eyJhbGciOiJSUzI1NiJ9.mock-refresh-token.signature',
  expires_in: 900,
};
