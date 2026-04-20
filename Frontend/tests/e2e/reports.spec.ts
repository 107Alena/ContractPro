// Reports e2e (FE-TASK-048 acceptance): авторизованный пользователь видит
// страницу /reports с ключевыми виджетами. MSW-фикстура /contracts по умолчанию
// не содержит записей с processing_status=READY (contractSummaries:
// ANALYZING/AWAITING_USER_INPUT/FAILED), поэтому таблица рендерится в empty-
// state — это всё равно подтверждает «таблица отчётов отображается».
//
// Полноценный happy-path с populated-таблицей требует расширения фикстур
// tests/msw/fixtures/contracts.ts — оставлен как follow-up.
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

test.describe('ReportsPage @smoke', () => {
  test('LAWYER на /reports видит заголовок + metrics + таблицу', async ({ page }) => {
    await loginAs(page, 'LAWYER');
    await spaNavigate(page, '/reports');

    const pageRoot = page.getByTestId('page-reports');
    await expect(pageRoot).toBeVisible();
    await expect(pageRoot.getByRole('heading', { name: 'Отчёты' })).toBeVisible();
    await expect(pageRoot.getByTestId('reports-metrics')).toBeVisible();
    await expect(pageRoot.getByTestId('reports-table')).toBeVisible();
    expect(new URL(page.url()).pathname).toBe('/reports');
  });

  test('?share=expired → ExpiredLinkBanner виден, Скрыть убирает param', async ({ page }) => {
    await loginAs(page, 'LAWYER');
    await spaNavigate(page, '/reports?share=expired');

    await expect(page.getByTestId('expired-link-banner')).toBeVisible();
    await page.getByTestId('expired-link-banner-dismiss').click();
    await expect(page.getByTestId('expired-link-banner')).toHaveCount(0);

    const url = new URL(page.url());
    expect(url.searchParams.get('share')).toBeNull();
  });
});
