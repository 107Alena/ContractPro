import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';

import { useOnlineStatus } from '@/shared/lib/use-online-status';
import { Button } from '@/shared/ui';

import { ErrorLayout } from './ui/ErrorLayout';

/**
 * /offline landing-страница. Auto-detect через navigator.onLine: когда
 * соединение восстановлено, показывает inline-CTA «Вернуться» (history -1).
 * Sticky-баннер «Нет соединения» в Topbar (§9.3) покрывает глобальный кейс
 * для авторизованных страниц — эта страница сфокусирована на ситуации,
 * когда пользователь попал на /offline явно (рука-роут, deep-link).
 */
export function Offline(): JSX.Element {
  const { t } = useTranslation(['errors', 'common']);
  const navigate = useNavigate();
  const online = useOnlineStatus();

  return (
    <ErrorLayout title={t('offline.title')} description={t('offline.description')}>
      <Button variant="secondary" onClick={() => window.location.reload()}>
        {t('common:actions.reload')}
      </Button>
      {online ? (
        <div
          data-testid="offline-online-hint"
          className="mt-4 flex w-full max-w-md flex-col items-center gap-2 rounded-md border border-success/30 bg-success/10 p-3 text-sm text-fg"
        >
          <p>{t('offline.onlineHint')}</p>
          <Button
            variant="primary"
            data-testid="offline-back-to-previous"
            onClick={() => {
              // Если /offline открыт как deep-link (history.length <= 1),
              // navigate(-1) не имеет куда вернуться — отправляем на /contracts.
              if (typeof window !== 'undefined' && window.history.length <= 1) {
                navigate('/contracts');
                return;
              }
              navigate(-1);
            }}
          >
            {t('offline.backToPrevious')}
          </Button>
        </div>
      ) : null}
    </ErrorLayout>
  );
}
