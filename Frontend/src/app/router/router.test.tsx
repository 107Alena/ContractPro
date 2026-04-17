// @vitest-environment jsdom
// createBrowserRouter требует DOM history; компонентный RouteError рендерится в RTL.
import '@/shared/i18n/config';

import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { I18nProvider } from '@/shared/i18n';

import { RouteError } from './RouteError';
import { createAppRouter, ROUTES } from './router';

afterEach(() => {
  cleanup();
});

describe('createAppRouter', () => {
  it('регистрирует все error-маршруты + root + wildcard', () => {
    const router = createAppRouter();
    const paths = router.routes.map((r) => r.path);
    expect(paths).toContain(ROUTES.root);
    expect(paths).toContain(ROUTES.forbidden);
    expect(paths).toContain(ROUTES.notFound);
    expect(paths).toContain(ROUTES.serverError);
    expect(paths).toContain(ROUTES.offline);
    expect(paths).toContain('*');
  });

  it('все error-маршруты имеют errorElement (fallback при loader-throw)', () => {
    const router = createAppRouter();
    const errorPaths = [
      ROUTES.root,
      ROUTES.forbidden,
      ROUTES.notFound,
      ROUTES.serverError,
      ROUTES.offline,
    ];
    for (const p of errorPaths) {
      const route = router.routes.find((r) => r.path === p) as
        | { errorElement?: unknown }
        | undefined;
      expect(route?.errorElement).toBeDefined();
    }
  });

  it('ROUTES константы соответствуют §6.1', () => {
    expect(ROUTES).toEqual({
      root: '/',
      forbidden: '/403',
      notFound: '/404',
      serverError: '/500',
      offline: '/offline',
    });
  });
});

describe('RouteError fallback', () => {
  it('рендерит заголовок и описание ошибки из ru/errors.json', () => {
    render(
      <I18nProvider>
        <RouteError />
      </I18nProvider>,
    );
    expect(screen.getByRole('heading', { name: 'Что-то пошло не так' })).toBeDefined();
    expect(screen.getByText(/Произошла непредвиденная ошибка/)).toBeDefined();
  });

  it('показывает кнопку Повторить (вызывает resetError)', () => {
    let called = 0;
    render(
      <I18nProvider>
        <RouteError
          resetError={() => {
            called += 1;
          }}
        />
      </I18nProvider>,
    );
    const retry = screen.getByRole('button', { name: 'Повторить' });
    retry.click();
    expect(called).toBe(1);
  });
});
