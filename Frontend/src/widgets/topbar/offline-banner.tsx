// Sticky offline-баннер для Topbar (§9.3 high-architecture). Рендерится как
// первый child внутри <header> виджета — чтобы не ломать sticky-offset общей
// шапки. Контент озвучивается screen-reader'ом через role="status" +
// aria-live="polite" (не «assertive», чтобы не перебивать активные диалоги).
import { useTranslation } from 'react-i18next';

import { cn } from '@/shared/lib/cn';

import { WifiOffIcon } from './icons';

export interface OfflineBannerProps {
  /** true = рендерим баннер; false = null. */
  visible: boolean;
  className?: string;
}

export function OfflineBanner({ visible, className }: OfflineBannerProps): JSX.Element | null {
  const { t } = useTranslation(['topbar']);

  if (!visible) return null;

  return (
    <div
      role="status"
      aria-live="polite"
      data-testid="topbar-offline-banner"
      className={cn(
        'flex items-center gap-2 border-b border-warning/30 bg-warning/10 px-6 py-2 text-sm text-fg',
        className,
      )}
    >
      <WifiOffIcon className="h-4 w-4 shrink-0 text-warning" aria-hidden="true" />
      <span className="sr-only">{t('topbar:offline.srAnnounce')}</span>
      <span>{t('topbar:offline.banner')}</span>
    </div>
  );
}
