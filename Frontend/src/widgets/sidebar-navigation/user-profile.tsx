// UserProfile — карточка пользователя внизу сайдбара (Figma 85:40) + popover с
// logout. Переехала из topbar/user-menu (этап 4.4 App Shell): в Figma профиль и
// выход живут в сайдбаре, а не в топбаре.
//
// Роли локализуются жёстко (как остальные строки сайдбара) — не через i18n.
// Logout делегирован features/auth useLogout (best-effort POST /auth/logout + clear).
import { useLogout } from '@/features/auth';
import { type User, useSession } from '@/shared/auth';
import { cn } from '@/shared/lib/cn';
import { Button } from '@/shared/ui';
import { Popover, PopoverContent, PopoverTrigger } from '@/shared/ui/popover';

import { LogOutIcon } from './icons';

const ROLE_LABEL: Record<User['role'], string> = {
  LAWYER: 'Юрист',
  BUSINESS_USER: 'Бизнес-пользователь',
  ORG_ADMIN: 'Администратор',
};

function getInitials(name: string): string {
  const parts = name.trim().split(/\s+/).slice(0, 2);
  const letters = parts.map((p) => p[0] ?? '').join('');
  return letters.toUpperCase() || '•';
}

export interface UserProfileProps {
  collapsed: boolean;
  /** Тест-оверрайд: рендерить пользователя без подключения к sessionStore. */
  user?: User | null;
  /** Начальное состояние open (для Storybook). */
  defaultOpen?: boolean;
}

export function UserProfile({
  collapsed,
  user: userProp,
  defaultOpen,
}: UserProfileProps): JSX.Element | null {
  const sessionUser = useSession((s) => s.user);
  const user = userProp !== undefined ? userProp : sessionUser;
  const { logout, isPending } = useLogout();

  if (!user) return null;

  const roleLabel = ROLE_LABEL[user.role] ?? user.role;

  return (
    <Popover {...(defaultOpen !== undefined ? { defaultOpen } : {})}>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label={`Профиль: ${user.name}`}
          data-testid="sidebar-user-trigger"
          className={cn(
            'flex items-center gap-2.5 rounded-[8px] text-left transition-colors hover:bg-bg-muted',
            'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2',
            collapsed ? 'mx-auto size-10 justify-center' : 'w-full px-2 py-1.5',
          )}
        >
          <span
            aria-hidden="true"
            className="inline-flex size-8 shrink-0 items-center justify-center rounded-full bg-brand-50 text-12 font-semibold text-brand-600"
          >
            {getInitials(user.name)}
          </span>
          {!collapsed && (
            <span className="flex min-w-0 flex-col">
              <span className="truncate text-13 font-medium text-fg-strong">{user.name}</span>
              <span className="truncate text-12 text-fg-disabled">{roleLabel}</span>
            </span>
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent size="sm" align="start" side="top" data-testid="sidebar-user-content">
        <div className="flex flex-col gap-3">
          <div className="min-w-0">
            <div className="truncate text-14 font-medium text-fg">{user.name}</div>
            <div className="truncate text-12 text-fg-muted">{user.email}</div>
          </div>
          <dl className="flex flex-col gap-2 text-12 text-fg-muted">
            <div className="flex items-start justify-between gap-3">
              <dt className="shrink-0">Роль</dt>
              <dd className="truncate text-right text-fg">{roleLabel}</dd>
            </div>
            <div className="flex items-start justify-between gap-3">
              <dt className="shrink-0">Организация</dt>
              <dd className="truncate text-right text-fg">{user.organization_name}</dd>
            </div>
          </dl>
          <Button
            variant="secondary"
            size="sm"
            data-testid="sidebar-logout"
            disabled={isPending}
            iconLeft={<LogOutIcon className="h-4 w-4" />}
            onClick={() => {
              void logout();
            }}
          >
            {isPending ? 'Выходим…' : 'Выйти'}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
