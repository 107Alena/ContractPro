import { createBrowserRouter, Navigate } from 'react-router-dom';

import { Forbidden403, NotFound404, Offline, ServerError500 } from '@/pages/errors';
import { LandingPage } from '@/pages/landing';

import { RouteError } from './RouteError';

export const ROUTES = {
  root: '/',
  forbidden: '/403',
  notFound: '/404',
  serverError: '/500',
  offline: '/offline',
} as const;

export type AppRoute = (typeof ROUTES)[keyof typeof ROUTES];

/**
 * Root-router. §6.1 high-architecture — error-страницы 403/404/500/offline
 * плюс плейсхолдер landing для `/`. Остальные маршруты добавляются
 * фича-тасками (FE-TASK-027/031/...).
 */
export function createAppRouter(): ReturnType<typeof createBrowserRouter> {
  return createBrowserRouter([
    {
      path: ROUTES.root,
      element: <LandingPage />,
      errorElement: <RouteError />,
    },
    {
      path: ROUTES.forbidden,
      element: <Forbidden403 />,
      errorElement: <RouteError />,
    },
    {
      path: ROUTES.notFound,
      element: <NotFound404 />,
      errorElement: <RouteError />,
    },
    {
      path: ROUTES.serverError,
      element: <ServerError500 />,
      errorElement: <RouteError />,
    },
    {
      path: ROUTES.offline,
      element: <Offline />,
      errorElement: <RouteError />,
    },
    {
      path: '*',
      element: <Navigate to={ROUTES.notFound} replace />,
    },
  ]);
}
