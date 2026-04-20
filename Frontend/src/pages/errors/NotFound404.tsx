import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';

import { Button } from '@/shared/ui';

import { ErrorLayout } from './ui/ErrorLayout';

export function NotFound404(): JSX.Element {
  const { t } = useTranslation(['errors', 'common']);
  const navigate = useNavigate();
  return (
    <ErrorLayout
      code={t('notFound.code')}
      title={t('notFound.title')}
      description={t('notFound.description')}
    >
      <Button variant="primary" onClick={() => navigate('/contracts')}>
        {t('common:actions.goToContracts')}
      </Button>
      <Button variant="secondary" onClick={() => navigate('/')}>
        {t('common:actions.home')}
      </Button>
    </ErrorLayout>
  );
}
