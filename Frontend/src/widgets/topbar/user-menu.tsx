// UserMenu — popover-dropdown: имя/роль/organization_name + кнопка «Выйти».
// Открывается по триггеру в Topbar. Логика logout делегирована
// features/auth/logout useLogout (best-effort POST /auth/logout + clear).
//
// Роль пользователя переводится через common:role.<UserRole>. Если бэкенд
// вернул неизвестную роль — показываем raw-значение (заодно видим drift
// OpenAPI vs. реальный ответ, не ломая UI).
import { useTranslation } from 'react-i18next';

import { useLogout } from '@/features/auth';
import { type User, useSession } from '@/shared/auth';
import { Button } from '@/shared/ui';
import { Popover, PopoverContent, PopoverTrigger } from '@/shared/ui/popover';

import { ChevronDownIcon, UserCircleIcon } from './icons';

export interface UserMenuProps {
  /** Тест-оверрайд: рендерить пользователя без подключения к sessionStore. */
  user?: User | null;
  /** Начальное состояние open (для Storybook). */
  defaultOpen?: boolean;
}

function getInitials(name: string): string {
  const parts = name.trim().split(/\s+/).slice(0, 2);
  const letters = parts.map((p) => p[0] ?? '').join('');
  return letters.toUpperCase() || '•';
}

export function UserMenu({ user: userProp, defaultOpen }: UserMenuProps = {}): JSX.Element | null {
  const { t } = useTranslation(['topbar', 'common']);
  const sessionUser = useSession((s) => s.user);
  const user = userProp !== undefined ? userProp : sessionUser;
  const { logout, isPending } = useLogout();

  // Не рендерим triggered-меню, если пользователь не загружен. Это избавляет
  // от flash-of-empty-trigger в первую секунду после login-redirect, пока
  // GET /users/me ещё не вернулся.
  if (!user) return null;

  // Роль: ключ вида common:role.LAWYER. Если нет — возвращаем сам ключ
  // роли (как fallback) — признак дрейфа enum'а в OpenAPI.
  const roleLabel = t(`common:role.${user.role}`, {
    defaultValue: user.role,
  });

  return (
    <Popover {...(defaultOpen !== undefined ? { defaultOpen } : {})}>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label={t('topbar:userMenu.trigger')}
          data-testid="topbar-user-menu-trigger"
          className="inline-flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-fg-muted transition-colors hover:bg-bg-muted hover:text-fg focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2"
        >
          <span
            aria-hidden="true"
            className="inline-flex h-8 w-8 items-center justify-center rounded-full bg-brand-50 text-xs font-semibold text-brand-600"
          >
            {getInitials(user.name)}
          </span>
          <span className="hidden max-w-[160px] truncate text-left md:inline-flex md:flex-col md:leading-tight">
            <span className="font-medium text-fg">{user.name}</span>
            <span className="text-xs text-fg-muted">{roleLabel}</span>
          </span>
          <ChevronDownIcon className="h-4 w-4" />
        </button>
      </PopoverTrigger>
      <PopoverContent size="sm" align="end" data-testid="topbar-user-menu-content">
        <div className="flex flex-col gap-3">
          <div className="flex items-start gap-3">
            <UserCircleIcon className="h-8 w-8 shrink-0 text-fg-muted" />
            <div className="min-w-0">
              <div className="truncate font-medium text-fg">{user.name}</div>
              <div className="truncate text-xs text-fg-muted">{user.email}</div>
            </div>
          </div>
          <dl className="space-y-2 text-xs text-fg-muted">
            <div className="flex items-start justify-between gap-3">
              <dt className="shrink-0">{t('topbar:userMenu.roleLabel')}</dt>
              <dd className="truncate text-right text-fg">{roleLabel}</dd>
            </div>
            <div className="flex items-start justify-between gap-3">
              <dt className="shrink-0">{t('topbar:userMenu.organizationLabel')}</dt>
              <dd className="truncate text-right text-fg">{user.organization_name}</dd>
            </div>
          </dl>
          <Button
            variant="secondary"
            data-testid="topbar-logout"
            disabled={isPending}
            onClick={() => {
              void logout();
            }}
          >
            {isPending ? t('topbar:userMenu.loggingOut') : t('topbar:userMenu.logout')}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
