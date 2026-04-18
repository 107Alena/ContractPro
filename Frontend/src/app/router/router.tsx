import type { ComponentType, ReactNode } from 'react';
import { lazy, Suspense } from 'react';
import type { RouteObject, UIMatch } from 'react-router-dom';
import { createBrowserRouter } from 'react-router-dom';

import { Forbidden403, NotFound404, Offline, ServerError500 } from '@/pages/errors';
import { LandingPage } from '@/pages/landing';

import { AdminLayout } from './AdminLayout';
import { AppLayout } from './AppLayout';
import { RouteError } from './RouteError';

// Lazy-компоненты подняты в module-scope — стабильные идентичности
// между вызовами buildRoutes() (важно для тестов и React.memo).
// Каждый dynamic import становится отдельным chunk-ом в Vite/Rollup.
const LoginPage = lazyComponent(() => import('@/pages/auth'), 'LoginPage');
const DashboardPage = lazyComponent(() => import('@/pages/dashboard'), 'DashboardPage');
const ContractsListPage = lazyComponent(
  () => import('@/pages/contracts-list'),
  'ContractsListPage',
);
const NewCheckPage = lazyComponent(() => import('@/pages/new-check'), 'NewCheckPage');
const ContractDetailPage = lazyComponent(
  () => import('@/pages/contract-detail'),
  'ContractDetailPage',
);
const ResultPage = lazyComponent(() => import('@/pages/result'), 'ResultPage');
const ComparisonPage = lazyComponent(() => import('@/pages/comparison'), 'ComparisonPage');
const ReportsPage = lazyComponent(() => import('@/pages/reports'), 'ReportsPage');
const SettingsPage = lazyComponent(() => import('@/pages/settings'), 'SettingsPage');
const AdminPoliciesPage = lazyComponent(
  () => import('@/pages/admin-policies'),
  'AdminPoliciesPage',
);
const AdminChecklistsPage = lazyComponent(
  () => import('@/pages/admin-checklists'),
  'AdminChecklistsPage',
);

function lazyComponent(
  loader: () => Promise<Record<string, ComponentType>>,
  exportName: string,
): ComponentType {
  return lazy(async () => {
    const mod = await loader();
    const Component = mod[exportName];
    if (!Component) {
      throw new Error(`lazyComponent: export "${exportName}" not found`);
    }
    return { default: Component };
  });
}

// TODO(FE-TASK-032): заменить fallback={null} на минимальный skeleton с
// role="status" aria-busy для a11y и предотвращения layout shift на slow 3G.
function lazyElement(Component: ComponentType): ReactNode {
  return (
    <Suspense fallback={null}>
      <Component />
    </Suspense>
  );
}

export const ROUTES = {
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
} as const;

export type AppRoute = (typeof ROUTES)[keyof typeof ROUTES];

/**
 * Тип route.handle.crumb (§6.4 high-architecture). Может быть статичной строкой
 * или функцией от UIMatch (для динамических сегментов вроде /contracts/:id).
 * Потребляется виджетом widgets/breadcrumbs (FE-TASK-033) через useMatches().
 */
export type CrumbResolver = (match: UIMatch) => string;
export type CrumbValue = string | CrumbResolver;
export interface RouteHandle {
  crumb: CrumbValue;
}

const handle = (crumb: CrumbValue): RouteHandle => ({ crumb });

/**
 * Root-router (§6.1 high-architecture). Аутентификация (RequireAuth) на
 * /dashboard, /contracts/*, /reports, /settings добавляется в FE-TASK-032
 * одновременно с AppLayout shell. В v1 RBAC route-guard — только
 * <RequireRole roles={['ORG_ADMIN']}> на /admin/* (§5.6 Pattern A) через AdminLayout.
 *
 * /audit (§17.1, §18 п.5) намеренно не зарегистрирован — отложен в v1.1 (FE-TASK-003).
 *
 * Loaders для /contracts/:id и /...result (§6.2) — TODO(FE-TASK-045/046):
 * привязать ensureQueryData(useContract/useVersions) после готовности FE-TASK-024.
 *
 * Code-splitting (§6.3, §11.2): React.lazy + Suspense. Не используем React Router 6.4
 * `lazy:` route-property API — он несовместим с createMemoryRouter под Node 20 + undici
 * (TypeError: RequestInit AbortSignal), что блокирует jsdom-тесты. React.lazy + Suspense
 * даёт идентичный chunk-splitting для production и стабилен в тестах.
 */
export function createAppRouter(): ReturnType<typeof createBrowserRouter> {
  return createBrowserRouter(buildRoutes());
}

export function buildRoutes(): RouteObject[] {
  return [
    {
      path: ROUTES.root,
      element: <LandingPage />,
      errorElement: <RouteError />,
      handle: handle('Главная'),
    },
    {
      path: ROUTES.login,
      element: lazyElement(LoginPage),
      errorElement: <RouteError />,
      handle: handle('Вход'),
    },
    {
      element: <AppLayout />,
      errorElement: <RouteError />,
      children: [
        {
          path: ROUTES.dashboard,
          element: lazyElement(DashboardPage),
          handle: handle('Главная'),
        },
        {
          path: ROUTES.contracts,
          element: lazyElement(ContractsListPage),
          handle: handle('Документы'),
        },
        {
          path: ROUTES.contractsNew,
          element: lazyElement(NewCheckPage),
          handle: handle('Новая проверка'),
        },
        {
          path: ROUTES.contractDetail,
          element: lazyElement(ContractDetailPage),
          handle: handle((m) => `Договор ${(m.params as { id?: string }).id ?? ''}`.trim()),
        },
        {
          path: ROUTES.result,
          element: lazyElement(ResultPage),
          handle: handle('Результат'),
        },
        {
          path: ROUTES.comparison,
          element: lazyElement(ComparisonPage),
          handle: handle('Сравнение версий'),
        },
        {
          path: ROUTES.reports,
          element: lazyElement(ReportsPage),
          handle: handle('Отчёты'),
        },
        {
          path: ROUTES.settings,
          element: lazyElement(SettingsPage),
          handle: handle('Настройки'),
        },
        {
          path: '/admin',
          element: <AdminLayout />,
          handle: handle('Администрирование'),
          children: [
            {
              path: 'policies',
              element: lazyElement(AdminPoliciesPage),
              handle: handle('Политики'),
            },
            {
              path: 'checklists',
              element: lazyElement(AdminChecklistsPage),
              handle: handle('Чек-листы'),
            },
          ],
        },
      ],
    },
    {
      path: ROUTES.forbidden,
      element: <Forbidden403 />,
      errorElement: <RouteError />,
      handle: handle('Нет доступа'),
    },
    {
      path: ROUTES.notFound,
      element: <NotFound404 />,
      errorElement: <RouteError />,
      handle: handle('Не найдено'),
    },
    {
      path: ROUTES.serverError,
      element: <ServerError500 />,
      errorElement: <RouteError />,
      handle: handle('Ошибка сервера'),
    },
    {
      path: ROUTES.offline,
      element: <Offline />,
      errorElement: <RouteError />,
      handle: handle('Нет соединения'),
    },
    {
      path: '*',
      element: <NotFound404 />,
      errorElement: <RouteError />,
      handle: handle('Не найдено'),
    },
  ];
}
