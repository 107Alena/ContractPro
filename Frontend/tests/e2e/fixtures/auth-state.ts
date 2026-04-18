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

import type { Page } from '@playwright/test';

import {
  __encodeRefreshTokenForTests,
  __REFRESH_STORAGE_KEY,
} from '@/processes/auth-flow/refresh-token-storage';

/** Тот же refresh-token, что MSW отдаёт в `validTokens` (tests/msw/fixtures/auth.ts). */
export const DEFAULT_MSW_REFRESH_TOKEN = 'eyJhbGciOiJSUzI1NiJ9.mock-refresh-token.signature';

/**
 * Сидирует sessionStorage до старта приложения. Вызывать ДО `page.goto(...)`.
 *
 * @example
 *   test.beforeEach(async ({ page }) => {
 *     await seedAuthenticatedSession(page);
 *     await page.goto('/dashboard');
 *   });
 */
export async function seedAuthenticatedSession(
  page: Page,
  refreshToken: string = DEFAULT_MSW_REFRESH_TOKEN,
): Promise<void> {
  const encoded = __encodeRefreshTokenForTests(refreshToken);
  // addInitScript выполняется в изолированном world'е до любого скрипта
  // страницы (включая HMR-клиент Vite), поэтому main.tsx увидит токен.
  await page.addInitScript(
    (payload: { key: string; value: string }) => {
      window.sessionStorage.setItem(payload.key, payload.value);
    },
    { key: __REFRESH_STORAGE_KEY, value: encoded },
  );
}
