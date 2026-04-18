// Smoke e2e (FE-TASK-055 acceptance #5): неавторизованный пользователь,
// заходящий на защищённый маршрут, должен быть перенаправлен на /login.
//
// Почему `/admin/policies`, а не `/`:
//   `/` в v1 — публичный LandingPage (§6.1 routing, placeholder от FE-TASK-030,
//   полноценная реализация — FE-TASK-041). RequireAuth-guard для `/dashboard`
//   появится позже. Сегодня единственный auth-gated маршрут с activated-гвардом
//   — `/admin/*` (через `<RequireRole>`, §5.6). Оба пути покрывают спирит
//   acceptance criteria «неавторизованный → /login».

import { expect, test } from './fixtures';

test.describe('Auth-gated redirect @smoke', () => {
  test('Неавторизованный GET /admin/policies → редирект на /login', async ({ page }) => {
    const response = await page.goto('/admin/policies');
    // Vite dev-сервер отдаст 200 даже на SPA-роуте — редирект делает React Router
    // клиентски после mount'а.
    expect(response?.status()).toBeLessThan(400);

    await page.waitForURL(/\/login(\?.*)?$/);

    const url = new URL(page.url());
    expect(url.pathname).toBe('/login');

    // Если присутствует redirect-параметр — только same-origin path (sanitizeRedirect).
    const redirect = url.searchParams.get('redirect');
    if (redirect !== null) {
      expect(redirect.startsWith('/')).toBe(true);
      expect(redirect.startsWith('//')).toBe(false);
    }

    // Контент LoginPage действительно отрендерен (маркер из pages/auth/LoginPage.tsx).
    await expect(page.getByTestId('page-login')).toBeVisible();
  });
});
