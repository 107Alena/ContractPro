// Topbar widget (§8.3 high-architecture): sticky шапка для всех защищённых
// экранов. Композирует: mobile-hamburger, optional search, user-menu, sticky
// offline-banner (§9.3). Sidebar остаётся отдельным sticky-колонкой слева.
//
// Notifications-кнопка присутствует как визуальный placeholder (пустой
// popover с текстом «Нет новых уведомлений») — контент наполняется после
// готовности backend-контракта notifications API (вне скоупа v1).
import { useTranslation } from 'react-i18next';

import { useLayoutStore } from '@/shared/layout';
import { cn } from '@/shared/lib/cn';
import { useOnlineStatus } from '@/shared/lib/use-online-status';
import { SearchInput } from '@/shared/ui';
import { Popover, PopoverContent, PopoverTrigger } from '@/shared/ui/popover';

import { BellIcon, MenuIcon } from './icons';
import { OfflineBanner } from './offline-banner';
import { UserMenu, type UserMenuProps } from './user-menu';

export interface TopbarProps {
  /**
   * Controlled search-input. Если передан объект с value+onChange — SearchInput
   * рендерится в центре топбара. Если undefined — левая часть схлопывается.
   * Поиск опционален (§17 таблица маршрутов), конкретные страницы передают
   * свои фильтры; Topbar — пассивный композер.
   */
  search?: {
    value: string;
    onChange: (value: string) => void;
  };
  /** Показать notification-кнопку. Default — false (v1). */
  withNotifications?: boolean;
  /** Тест-оверрайд для UserMenu (изолированные stories/tests). */
  userMenuProps?: UserMenuProps;
  /** Тест-оверрайд online-статуса. undefined = реальный navigator.onLine. */
  forceOffline?: boolean;
  className?: string;
}

export function Topbar({
  search,
  withNotifications = false,
  userMenuProps,
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
      // z-20 (Tailwind default) < z-modal (1000): sticky header не должен
      // перекрывать модалки из shared/ui/modal (они используют z-modal).
      className={cn('sticky top-0 z-20 bg-bg/95 backdrop-blur border-b border-border', className)}
    >
      <OfflineBanner visible={isOffline} />
      <div className="flex h-14 items-center gap-3 px-4 md:px-6">
        <button
          type="button"
          onClick={openMobileDrawer}
          aria-label={t('topbar:mobile.openMenu')}
          data-testid="topbar-mobile-menu"
          className="inline-flex h-9 w-9 items-center justify-center rounded-md text-fg-muted hover:bg-bg-muted hover:text-fg focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2 md:hidden"
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
              className="max-w-xl"
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
                className="inline-flex h-9 w-9 items-center justify-center rounded-md text-fg-muted hover:bg-bg-muted hover:text-fg focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2"
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

        <UserMenu {...userMenuProps} />
      </div>
    </header>
  );
}
