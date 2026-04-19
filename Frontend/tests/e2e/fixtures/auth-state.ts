// Auth-state helper для e2e-сценариев, требующих авторизованного пользователя
// (FE-TASK-055 acceptance #3, «auth-state как storage state»).
//
// Проблема: приложение держит refresh-token в `sessionStorage` (ADR-FE-03,
// key `cp.rt.v1`, XOR+base64-обфускация). Playwright `storageState` сохраняет
// только cookies и `localStorage` — sessionStorage намеренно не включён
// (tab-scoped). Решение — `page.addInitScript()`, запускаемый до первого
// `document` JS: сидируем sessionStorage до того, как стартует `main.tsx`,
// и `initAuthFlow` подхватит refresh-токен на первом silent-refresh.
//
// Значение refresh-токена синхронизировано с `tests/msw/fixtures/auth.ts`
// (`validTokens.refresh_token`) — MSW handler `POST /auth/refresh` отдаст
// валидный access + обновлённый refresh при первом вызове.
//
// Encoding переиспользуется из `src/processes/auth-flow/refresh-token-storage.ts`
// через `__encodeRefreshTokenForTests` — исключает silent-drift ключа и XOR'а,
// если прод-код мигрирует (напр., в HttpOnly cookie per §18 п.1).
//
// Role override (FE-TASK-001): параметр `role` кладётся в window.__cpE2eRole__
// до старта приложения. main.tsx после worker.start() применяет его через
// setE2EUserRole → GET /users/me возвращает соответствующий fixture.
// Cookie-based подход не работает: MSW service worker не получает Cookie-заголовок
// в Request (ограничение Service Worker Fetch API).

import type { Page } from '@playwright/test';

import {
  __encodeRefreshTokenForTests,
  __REFRESH_STORAGE_KEY,
} from '@/processes/auth-flow/refresh-token-storage';

/** Тот же refresh-token, что MSW отдаёт в `validTokens` (tests/msw/fixtures/auth.ts). */
export const DEFAULT_MSW_REFRESH_TOKEN = 'eyJhbGciOiJSUzI1NiJ9.mock-refresh-token.signature';

export type E2ERole = 'LAWYER' | 'BUSINESS_USER' | 'ORG_ADMIN';

export interface SeedAuthOptions {
  refreshToken?: string;
  role?: E2ERole;
}

/**
 * Сидирует sessionStorage + опциональный MSW role-override до старта приложения.
 * Вызывать ДО `page.goto(...)`.
 *
 * @example
 *   test.beforeEach(async ({ page }) => {
 *     await seedAuthenticatedSession(page, { role: 'ORG_ADMIN' });
 *     await page.goto('/admin/policies');
 *   });
 */
export async function seedAuthenticatedSession(
  page: Page,
  options: SeedAuthOptions = {},
): Promise<void> {
  const refreshToken = options.refreshToken ?? DEFAULT_MSW_REFRESH_TOKEN;
  const encoded = __encodeRefreshTokenForTests(refreshToken);
  // addInitScript выполняется в изолированном world'е до любого скрипта
  // страницы (включая HMR-клиент Vite), поэтому main.tsx увидит и токен,
  // и role-override до worker.start().
  await page.addInitScript(
    (payload: { storageKey: string; storageValue: string; role?: E2ERole }) => {
      window.sessionStorage.setItem(payload.storageKey, payload.storageValue);
      if (payload.role) {
        (window as unknown as { __cpE2eRole__?: E2ERole }).__cpE2eRole__ = payload.role;
      }
    },
    {
      storageKey: __REFRESH_STORAGE_KEY,
      storageValue: encoded,
      ...(options.role !== undefined && { role: options.role }),
    },
  );
}
