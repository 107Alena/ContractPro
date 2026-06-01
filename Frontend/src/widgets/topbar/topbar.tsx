// Topbar — sticky шапка для защищённых экранов (Figma 86:3, restyle этап 4.4.2).
// Композирует: mobile-hamburger, optional search, optional notifications, sticky
// offline-banner (§9.3). Профиль и logout ПЕРЕЕХАЛИ в сайдбар
// (widgets/sidebar-navigation/user-profile) — в Figma 86:3 user-menu нет.
//
// Notifications — визуальный placeholder (popover «Нет новых уведомлений») до
// готовности backend notifications API (вне v1). Глобальный поиск/help из Figma
// не добавлены — нет бэкенда (scope 4.4: restyle без фейков).
import { useTranslation } from 'react-i18next';

import { useLayoutStore } from '@/shared/layout';
import { cn } from '@/shared/lib/cn';
import { useOnlineStatus } from '@/shared/lib/use-online-status';
import { SearchInput } from '@/shared/ui';
import { Popover, PopoverContent, PopoverTrigger } from '@/shared/ui/popover';

import { BellIcon, MenuIcon } from './icons';
import { OfflineBanner } from './offline-banner';

export interface TopbarProps {
  /**
   * Controlled search-input. Если передан объект с value+onChange — SearchInput
   * рендерится слева. Если undefined — левая часть схлопывается. Поиск опционален
   * (§17 таблица маршрутов); конкретные страницы передают свои фильтры.
   */
  search?: {
    value: string;
    onChange: (value: string) => void;
  };
  /** Показать notification-кнопку. Default — false (v1). */
  withNotifications?: boolean;
  /** Тест-оверрайд online-статуса. undefined = реальный navigator.onLine. */
  forceOffline?: boolean;
  className?: string;
}

// Figma 86:3: квадратная icon-кнопка bg-bg-muted rounded-8 size-9.
const ICON_BUTTON = cn(
  'inline-flex size-9 items-center justify-center rounded-[8px] bg-bg-muted text-fg-muted',
  'transition-colors hover:bg-border-subtle hover:text-fg',
  'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2',
);

export function Topbar({
  search,
  withNotifications = false,
  forceOffline,
  className,
}: TopbarProps = {}): JSX.Element {
  const { t } = useTranslation(['topbar']);
  const openMobileDrawer = useLayoutStore((s) => s.openMobileDrawer);
  const online = useOnlineStatus();
  const isOffline = forceOffline ?? !online;

  return (
    <header
      data-testid="topbar"
      // z-20 < z-modal (1000): sticky header не должен перекрывать модалки.
      className={cn(
        'sticky top-0 z-20 border-b border-border-subtle bg-bg/95 backdrop-blur',
        className,
      )}
    >
      <OfflineBanner visible={isOffline} />
      <div className="flex h-[60px] items-center gap-3 px-4 md:px-8">
        <button
          type="button"
          onClick={openMobileDrawer}
          aria-label={t('topbar:mobile.openMenu')}
          data-testid="topbar-mobile-menu"
          className={cn(ICON_BUTTON, 'md:hidden')}
        >
          <MenuIcon className="h-5 w-5" />
        </button>

        {search ? (
          <div className="flex min-w-0 flex-1">
            <SearchInput
              value={search.value}
              onValueChange={search.onChange}
              placeholder={t('topbar:search.placeholder')}
              ariaLabel={t('topbar:search.label')}
              clearLabel={t('topbar:search.clearLabel')}
              className="max-w-[360px]"
              data-testid="topbar-search"
            />
          </div>
        ) : (
          <div className="flex-1" />
        )}

        {withNotifications ? (
          <Popover>
            <PopoverTrigger asChild>
              <button
                type="button"
                aria-label={t('topbar:notifications.trigger')}
                data-testid="topbar-notifications-trigger"
                className={ICON_BUTTON}
              >
                <BellIcon className="h-5 w-5" />
              </button>
            </PopoverTrigger>
            <PopoverContent size="sm" align="end">
              <p className="text-sm text-fg-muted" data-testid="topbar-notifications-empty">
                {t('topbar:notifications.empty')}
              </p>
            </PopoverContent>
          </Popover>
        ) : null}
      </div>
    </header>
  );
}
