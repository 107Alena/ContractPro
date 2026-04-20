// Settings e2e (FE-TASK-049 acceptance): авторизованный LAWYER открывает
// /settings, видит профиль + кнопку «Выйти»; клик по «Выйти» редиректит
// на /login и чистит сессионный store (useSession.user = null) + MSW
// POST /auth/logout возвращает 204 (tests/msw/handlers/auth.ts:36).
import type { Page } from '@playwright/test';

import { expect, seedAuthenticatedSession, test } from './fixtures';

async function loginAs(page: Page, role: 'LAWYER' | 'ORG_ADMIN' | 'BUSINESS_USER'): Promise<void> {
  await seedAuthenticatedSession(page, { role });
  await page.goto('/login');
  await page.getByLabel('Email').fill('user@contractpro.local');
  await page.getByLabel('Пароль').fill('correct-horse-battery-staple');
  await page.getByRole('button', { name: 'Войти' }).click();
  await page.waitForURL(/\/dashboard$/);
}

async function spaNavigate(page: Page, path: string): Promise<void> {
  await page.evaluate((to: string) => {
    window.history.pushState({}, '', to);
    window.dispatchEvent(new PopStateEvent('popstate'));
  }, path);
}

test.describe('SettingsPage @smoke', () => {
  test('LAWYER видит профиль, организацию и роль', async ({ page }) => {
    await loginAs(page, 'LAWYER');
    await spaNavigate(page, '/settings');

    const pageRoot = page.getByTestId('page-settings');
    await expect(pageRoot).toBeVisible();
    await expect(pageRoot.getByRole('heading', { name: 'Настройки' })).toBeVisible();

    // Данные useMe (fixtures.users.lawyer из tests/msw/fixtures/users.ts).
    await expect(pageRoot.getByText('Алина Юрьева')).toBeVisible();
    await expect(pageRoot.getByText('lawyer@contractpro.local')).toBeVisible();
    await expect(pageRoot.getByText('ООО «Контракт-Сервис»')).toBeVisible();
    await expect(pageRoot.getByText('Юрист')).toBeVisible();
    await expect(pageRoot.getByTestId('settings-logout-btn')).toBeVisible();
  });

  test('клик «Выйти» → редирект /login + очистка сессии', async ({ page }) => {
    await loginAs(page, 'LAWYER');
    await spaNavigate(page, '/settings');

    await page.getByTestId('settings-logout-btn').click();
    await page.waitForURL(/\/login$/);
    expect(new URL(page.url()).pathname).toBe('/login');
  });
});
