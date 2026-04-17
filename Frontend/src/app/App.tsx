import { useMemo } from 'react';
import { RouterProvider } from 'react-router-dom';

import { createAppRouter } from '@/app/router';
import { I18nProvider } from '@/shared/i18n';
import { Toaster, TooltipProvider } from '@/shared/ui';

import { AppErrorBoundary } from './providers/AppErrorBoundary';
import { QueryProvider } from './providers/QueryProvider';

/**
 * Composition root (§1.1-§9, FE-TASK-030).
 * Порядок: ErrorBoundary → Query → I18n → Tooltip → Router; Toaster сиблинг Router.
 * Router создаётся через useMemo: читает window.location.href на первом рендере —
 * это корректно для SPA и позволяет тестам переключать URL через pushState.
 */
export function App(): JSX.Element {
  const router = useMemo(() => createAppRouter(), []);
  return (
    <AppErrorBoundary>
      <QueryProvider>
        <I18nProvider>
          <TooltipProvider delayDuration={500}>
            <RouterProvider router={router} />
            <Toaster />
          </TooltipProvider>
        </I18nProvider>
      </QueryProvider>
    </AppErrorBoundary>
  );
}
