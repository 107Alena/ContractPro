import { useTranslation } from 'react-i18next';

import { Button } from '@/shared/ui';

import { ErrorLayout } from './ui/ErrorLayout';

export function Offline(): JSX.Element {
  const { t } = useTranslation(['errors', 'common']);
  return (
    <ErrorLayout title={t('offline.title')} description={t('offline.description')}>
      <Button variant="secondary" onClick={() => window.location.reload()}>
        {t('common:actions.reload')}
      </Button>
    </ErrorLayout>
  );
}
