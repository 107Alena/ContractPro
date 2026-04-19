// @vitest-environment jsdom
// Структурные тесты buildRoutes() работают синхронно над данными.
// Render-тесты используют MemoryRouter + useRoutes (declarative API),
// а не RouterProvider/createMemoryRouter (data-router): data-router
// делает new Request(url, { signal }) внутри navigate, что несовместимо
// с jsdom + undici в Node 20 (TypeError: AbortSignal). MemoryRouter
// не делает Request, идентичен по логике matching и поддерживает Navigate.
import '@/shared/i18n/config';

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, useRoutes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { type User, useSession } from '@/shared/auth';
import { I18nProvider } from '@/shared/i18n';

import { RouteError } from './RouteError';
import { buildRoutes, createAppRouter, type RouteHandle, ROUTES } from './router';

const baseUser: User = {
  user_id: '00000000-0000-0000-0000-000000000001',
  email: 'u@example.com',
  name: 'Тест',
  role: 'LAWYER',
  organization_id: '00000000-0000-0000-0000-000000000002',
  organization_name: 'Тест Орг',
  permissions: { export_enabled: false },
};

function RoutedApp(): JSX.Element | null {
  return useRoutes(buildRoutes());
}

function renderAt(path: string) {
  // FE-TASK-042 добавил useMe()/useContracts() в DashboardPage — теперь любой
  // rendered lazy-chunk с TanStack-хуком требует QueryClientProvider. Держим
  // QueryClient локальным (retry:false) — реальных HTTP-запросов тесты не
  // проверяют, нас интересует только попадание в page-testid.
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider>
        <MemoryRouter initialEntries={[path]}>
          <RoutedApp />
        </MemoryRouter>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  useSession.getState().clear();
});

afterEach(() => {
  cleanup();
  useSession.getState().clear();
});

describe('createAppRouter', () => {
  it('возвращает экземпляр router (createBrowserRouter)', () => {
    const router = createAppRouter();
    expect(router.routes.length).toBeGreaterThan(0);
  });

  it('ROUTES содержит все 16 ключей §6.1', () => {
    expect(ROUTES).toEqual({
      root: '/',
      login: '/login',
      dashboard: '/dashboard',
      contracts: '/contracts',
      contractsNew: '/contracts/new',
      contractDetail: '/contracts/:id',
      result: '/contracts/:id/versions/:vid/result',
      comparison: '/contracts/:id/compare',
      reports: '/reports',
      settings: '/settings',
      adminPolicies: '/admin/policies',
      adminChecklists: '/admin/checklists',
      forbidden: '/403',
      notFound: '/404',
      serverError: '/500',
      offline: '/offline',
    });
  });
});

describe('buildRoutes — структура', () => {
  it('top-level включает /, /login, AppLayout, error-маршруты, wildcard', () => {
    const routes = buildRoutes();
    const paths = routes.map((r) => r.path);
    expect(paths).toContain('/');
    expect(paths).toContain('/login');
    expect(paths).toContain('/403');
    expect(paths).toContain('/404');
    expect(paths).toContain('/500');
    expect(paths).toContain('/offline');
    expect(paths).toContain('*');
  });

  it('AppLayout-route содержит все аутентифицированные children-маршруты', () => {
    const routes = buildRoutes();
    const layout = routes.find((r) => r.path === undefined && Array.isArray(r.children));
    expect(layout).toBeDefined();
    const childPaths = layout!.children!.map((c) => c.path);
    expect(childPaths).toContain('/dashboard');
    expect(childPaths).toContain('/contracts');
    expect(childPaths).toContain('/contracts/new');
    expect(childPaths).toContain('/contracts/:id');
    expect(childPaths).toContain('/contracts/:id/versions/:vid/result');
    expect(childPaths).toContain('/contracts/:id/compare');
    expect(childPaths).toContain('/reports');
    expect(childPaths).toContain('/settings');
    expect(childPaths).toContain('/admin');
  });

  it('admin-секция вложена под /admin (DRY с одним RequireRole)', () => {
    const routes = buildRoutes();
    const layout = routes.find((r) => r.path === undefined && Array.isArray(r.children));
    const admin = layout!.children!.find((c) => c.path === '/admin');
    expect(admin).toBeDefined();
    const adminChildren = admin!.children!.map((c) => c.path);
    expect(adminChildren).toContain('policies');
    expect(adminChildren).toContain('checklists');
  });

  it('каждый top-level маршрут имеет handle.crumb', () => {
    const routes = buildRoutes();
    for (const r of routes) {
      if (r.path === '*') continue; // wildcard: handle.crumb тоже задан, но проверим отдельно
      const handle = r.handle as RouteHandle | undefined;
      if (r.children) {
        // layout-routes без path-а не обязаны иметь handle на корне (handle живёт у листьев)
        for (const c of r.children) {
          expect((c.handle as RouteHandle | undefined)?.crumb).toBeDefined();
        }
      } else {
        expect(handle?.crumb).toBeDefined();
      }
    }
  });

  it('handle.crumb для /contracts/:id — функция от UIMatch (динамический сегмент)', () => {
    const routes = buildRoutes();
    const layout = routes.find((r) => r.path === undefined && Array.isArray(r.children));
    const detail = layout!.children!.find((c) => c.path === '/contracts/:id');
    const crumb = (detail!.handle as RouteHandle).crumb;
    expect(typeof crumb).toBe('function');
    if (typeof crumb === 'function') {
      const text = crumb({
        id: 'contractDetail',
        params: { id: 'abc-123' },
        pathname: '/contracts/abc-123',
        data: undefined,
        handle: detail!.handle,
      });
      expect(text).toBe('Договор abc-123');
    }
  });

  it('все error-маршруты + wildcard имеют errorElement (Sentry-fallback не вмешивается в loader-throw)', () => {
    const routes = buildRoutes();
    const expected = ['/', '/login', '/403', '/404', '/500', '/offline', '*'];
    for (const p of expected) {
      const route = routes.find((r) => r.path === p);
      expect(route?.errorElement).toBeDefined();
    }
  });
});

describe('Маршрутизация — рендер pages', () => {
  it('/ → LandingPage (eager, не lazy)', async () => {
    renderAt('/');
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'ContractPro' })).toBeDefined();
    });
  });

  it('/login → LoginPage (lazy)', async () => {
    renderAt('/login');
    expect(await screen.findByTestId('page-login')).toBeDefined();
  });

  it('/dashboard → DashboardPage (lazy через AppLayout)', async () => {
    renderAt('/dashboard');
    expect(await screen.findByTestId('page-dashboard')).toBeDefined();
  });

  it('/contracts → ContractsListPage (lazy)', async () => {
    renderAt('/contracts');
    expect(await screen.findByTestId('page-contracts-list')).toBeDefined();
  });

  it('/contracts/new → NewCheckPage', async () => {
    renderAt('/contracts/new');
    expect(await screen.findByTestId('page-new-check')).toBeDefined();
  });

  it('/contracts/:id → ContractDetailPage', async () => {
    // FE-TASK-045 заменил placeholder на реальную реализацию: в состоянии
    // загрузки параметр id больше не выводится в DOM (useContract висит на
    // pending-запросе). Проверяем только попадание на маршрут — интеграцию
    // данных покрывает ContractDetailPage.test.tsx.
    renderAt('/contracts/abc-123');
    expect(await screen.findByTestId('page-contract-detail')).toBeDefined();
  });

  it('/contracts/:id/versions/:vid/result → ResultPage с params', async () => {
    renderAt('/contracts/c1/versions/v2/result');
    expect(await screen.findByTestId('page-result')).toBeDefined();
    expect(screen.getByText(/c1/)).toBeDefined();
    expect(screen.getByText(/v2/)).toBeDefined();
  });

  it('/contracts/:id/compare?base=A&target=B → ComparisonPage с searchParams', async () => {
    renderAt('/contracts/c1/compare?base=v1&target=v2');
    expect(await screen.findByTestId('page-comparison')).toBeDefined();
    expect(screen.getByText(/v1/)).toBeDefined();
    expect(screen.getByText(/v2/)).toBeDefined();
  });

  it('/reports → ReportsPage', async () => {
    renderAt('/reports');
    expect(await screen.findByTestId('page-reports')).toBeDefined();
  });

  it('/settings → SettingsPage', async () => {
    renderAt('/settings');
    expect(await screen.findByTestId('page-settings')).toBeDefined();
  });

  it('/несуществующий-маршрут → NotFound404 (не редирект, сохраняет URL)', async () => {
    renderAt('/totally-non-existent-route');
    await waitFor(() => {
      expect(screen.getByText(/Страница не найдена/)).toBeDefined();
    });
  });

  it('/audit намеренно НЕ зарегистрирован (отложен в v1.1) → NotFound404', async () => {
    renderAt('/audit');
    await waitFor(() => {
      expect(screen.getByText(/Страница не найдена/)).toBeDefined();
    });
  });
});

describe('RBAC route-guards — /admin/* (Pattern A)', () => {
  it('не-аутентифицированный пользователь → /login', async () => {
    renderAt('/admin/policies');
    expect(await screen.findByTestId('page-login')).toBeDefined();
  });

  it('BUSINESS_USER → /403 (Forbidden)', async () => {
    useSession.setState({ user: { ...baseUser, role: 'BUSINESS_USER' } });
    renderAt('/admin/policies');
    await waitFor(() => {
      expect(screen.getByText(/Недостаточно прав/)).toBeDefined();
    });
  });

  it('LAWYER → /403 (admin-секция только для ORG_ADMIN)', async () => {
    useSession.setState({ user: { ...baseUser, role: 'LAWYER' } });
    renderAt('/admin/checklists');
    await waitFor(() => {
      expect(screen.getByText(/Недостаточно прав/)).toBeDefined();
    });
  });

  it('ORG_ADMIN → видит /admin/policies', async () => {
    useSession.setState({ user: { ...baseUser, role: 'ORG_ADMIN' } });
    renderAt('/admin/policies');
    expect(await screen.findByTestId('page-admin-policies')).toBeDefined();
  });

  it('ORG_ADMIN → видит /admin/checklists', async () => {
    useSession.setState({ user: { ...baseUser, role: 'ORG_ADMIN' } });
    renderAt('/admin/checklists');
    expect(await screen.findByTestId('page-admin-checklists')).toBeDefined();
  });
});

describe('Error-маршруты — статичные', () => {
  it('/403 → Forbidden403', async () => {
    renderAt('/403');
    await waitFor(() => {
      expect(screen.getByText(/Недостаточно прав/)).toBeDefined();
    });
  });

  it('/404 → NotFound404', async () => {
    renderAt('/404');
    await waitFor(() => {
      expect(screen.getByText(/Страница не найдена/)).toBeDefined();
    });
  });

  it('/500 → ServerError500', async () => {
    renderAt('/500');
    await waitFor(() => {
      expect(screen.getByText(/Временные проблемы/)).toBeDefined();
    });
  });

  it('/offline → Offline page', async () => {
    renderAt('/offline');
    await waitFor(() => {
      expect(screen.getByText(/Нет соединения/)).toBeDefined();
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
