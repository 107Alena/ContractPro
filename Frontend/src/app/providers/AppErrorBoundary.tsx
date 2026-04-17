import type { ReactNode } from 'react';

import { RouteError } from '@/app/router';
import { Sentry } from '@/shared/observability';

interface AppErrorBoundaryProps {
  children: ReactNode;
}

/**
 * Route-level ErrorBoundary (§9.2). Ловит исключения из всего поддерева
 * RouterProvider. При пустом DSN Sentry работает как no-op, но сам
 * компонент ErrorBoundary остаётся функциональным (локальный fallback).
 */
export function AppErrorBoundary({ children }: AppErrorBoundaryProps): JSX.Element {
  return (
    <Sentry.ErrorBoundary
      fallback={({ resetError, error }) => <RouteError error={error} resetError={resetError} />}
    >
      {children}
    </Sentry.ErrorBoundary>
  );
}
