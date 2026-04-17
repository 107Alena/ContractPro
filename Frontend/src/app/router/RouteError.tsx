import { useTranslation } from 'react-i18next';

import { ErrorLayout } from '@/pages/errors';
import { Button } from '@/shared/ui';

export interface RouteErrorProps {
  error?: unknown;
  resetError?: () => void;
}

/**
 * Fallback для Sentry.ErrorBoundary (§9.2 high-architecture).
 * Рендерится когда внутри RouterProvider брошено необработанное исключение.
 */
export function RouteError({ resetError }: RouteErrorProps): JSX.Element {
  const { t } = useTranslation(['errors', 'common']);

  const handleReset = () => {
    if (resetError) {
      resetError();
    } else {
      window.location.reload();
    }
  };

  return (
    <ErrorLayout title={t('route.title')} description={t('route.description')}>
      <Button variant="secondary" onClick={handleReset}>
        {t('common:actions.retry')}
      </Button>
      <Button variant="ghost" onClick={() => window.location.reload()}>
        {t('common:actions.reload')}
      </Button>
    </ErrorLayout>
  );
}
