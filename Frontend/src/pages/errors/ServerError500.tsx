import { useTranslation } from 'react-i18next';
import { useLocation } from 'react-router-dom';

import { Button } from '@/shared/ui';

import { ErrorLayout } from './ui/ErrorLayout';

interface LocationState {
  correlationId?: string;
}

export function ServerError500(): JSX.Element {
  const { t } = useTranslation(['errors', 'common']);
  const location = useLocation();
  const state = location.state as LocationState | null;
  const correlationId = state?.correlationId;

  return (
    <ErrorLayout
      code={t('serverError.code')}
      title={t('serverError.title')}
      description={t('serverError.description')}
    >
      <Button variant="secondary" onClick={() => window.location.reload()}>
        {t('common:actions.reload')}
      </Button>
      {correlationId ? (
        <div
          className="mt-4 w-full max-w-md rounded-md border border-border bg-bg-muted p-3 text-left text-xs text-fg-muted"
          data-testid="correlation-id"
        >
          <div className="font-medium text-fg">{t('serverError.correlationIdLabel')}</div>
          <code className="mt-1 block break-all">{correlationId}</code>
        </div>
      ) : null}
    </ErrorLayout>
  );
}
