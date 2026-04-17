import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';

import { Button } from '@/shared/ui';

import { ErrorLayout } from './ui/ErrorLayout';

export function Forbidden403(): JSX.Element {
  const { t } = useTranslation(['errors', 'common']);
  const navigate = useNavigate();
  return (
    <ErrorLayout
      code={t('forbidden.code')}
      title={t('forbidden.title')}
      description={t('forbidden.description')}
    >
      <Button variant="secondary" onClick={() => navigate('/')}>
        {t('common:actions.home')}
      </Button>
    </ErrorLayout>
  );
}
