// @vitest-environment jsdom
import '@/shared/i18n/config';

import { cleanup, render, screen } from '@testing-library/react';
import type { RouteObject } from 'react-router-dom';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';

import { I18nProvider } from '@/shared/i18n';

import { AppBreadcrumbs } from './breadcrumbs';

afterEach(() => {
  cleanup();
});

function renderAtPath(routes: RouteObject[], path: string): void {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  render(
    <I18nProvider>
      <RouterProvider router={router} />
    </I18nProvider>,
  );
}

describe('AppBreadcrumbs (widget)', () => {
  it('рендерит хлебные крошки по route.handle.crumb', () => {
    const routes: RouteObject[] = [
      {
        path: '/',
        element: <AppBreadcrumbs />,
        handle: { crumb: 'Главная' },
      },
    ];
    renderAtPath(routes, '/');
    const nav = screen.getByRole('navigation', { name: 'Хлебные крошки' });
    expect(nav).toBeDefined();
    expect(screen.getByText('Главная')).toBeDefined();
  });

  it('последний crumb имеет aria-current="page" и не является ссылкой', () => {
    const routes: RouteObject[] = [
      {
        path: '/',
        element: <AppBreadcrumbs />,
        handle: { crumb: 'Главная' },
        children: [
          {
            path: 'contracts',
            element: <AppBreadcrumbs />,
            handle: { crumb: 'Документы' },
          },
        ],
      },
    ];
    renderAtPath(routes, '/contracts');
    const current = screen.getAllByText('Документы')[0];
    expect(current?.getAttribute('aria-current')).toBe('page');
    expect(screen.queryByRole('link', { name: 'Документы' })).toBeNull();
  });

  it('поддерживает функциональный crumb с параметрами', () => {
    const routes: RouteObject[] = [
      {
        path: '/contracts/:id',
        element: <AppBreadcrumbs />,
        handle: { crumb: (m: { params: { id?: string } }) => `Договор ${m.params.id ?? ''}` },
      },
    ];
    renderAtPath(routes, '/contracts/42');
    expect(screen.getByText('Договор 42')).toBeDefined();
  });

  it('возвращает null когда нет совместимых matches', () => {
    const routes: RouteObject[] = [
      {
        path: '/',
        element: <AppBreadcrumbs />,
      },
    ];
    renderAtPath(routes, '/');
    expect(screen.queryByTestId('app-breadcrumbs')).toBeNull();
  });

  it('использует items-override без обращения к router', () => {
    const routes: RouteObject[] = [
      {
        path: '/',
        element: (
          <AppBreadcrumbs
            items={[
              { id: 'a', label: 'A', href: '/a', current: false },
              { id: 'b', label: 'B', current: true },
            ]}
          />
        ),
      },
    ];
    renderAtPath(routes, '/');
    expect(screen.getByText('A')).toBeDefined();
    expect(screen.getByText('B')).toBeDefined();
    expect(screen.getByRole('link', { name: 'A' })).toBeDefined();
  });
});
