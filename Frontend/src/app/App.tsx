import { useMemo } from 'react';
import { RouterProvider } from 'react-router-dom';

import { createAppRouter } from '@/app/router';
import { LowConfidenceConfirmProvider } from '@/features/low-confidence-confirm';
import { I18nProvider } from '@/shared/i18n';
import { Toaster, TooltipProvider } from '@/shared/ui';

import { AppErrorBoundary } from './providers/AppErrorBoundary';
import { QueryProvider } from './providers/QueryProvider';

/**
 * Composition root (§1.1-§9, FE-TASK-030).
 * Порядок: ErrorBoundary → Query → I18n → Tooltip → Router; Toaster сиблинг Router.
 * Router создаётся через useMemo: читает window.location.href на первом рендере —
 * это корректно для SPA и позволяет тестам переключать URL через pushState.
 *
 * LowConfidenceConfirmProvider — глобальный listener SSE `type_confirmation_required`
 * (FR-2.1.3). Должен быть внутри QueryProvider (использует useEventStream
 * → useQueryClient) и одновременно с RouterProvider (overlay-modal независим
 * от текущей страницы). Не оборачивает Router — модалка нейтральна к маршруту.
 */
export function App(): JSX.Element {
  const router = useMemo(() => createAppRouter(), []);
  return (
    <AppErrorBoundary>
      <QueryProvider>
        <I18nProvider>
          <TooltipProvider delayDuration={500}>
            <RouterProvider router={router} />
            <LowConfidenceConfirmProvider />
            <Toaster />
          </TooltipProvider>
        </I18nProvider>
      </QueryProvider>
    </AppErrorBoundary>
  );
}
