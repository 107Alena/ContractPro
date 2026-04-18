// OrgCard — карточка организации/пользователя на dashboard (§17.4).
// Использует данные useMe: имя, роль, организация. Пока не отображает
// лимиты/биллинг — §18 Payment Processing фичи в бэклоге.
import { type UserProfile } from '@/entities/user';
import { Badge, Spinner } from '@/shared/ui';

export interface OrgCardProps {
  user?: UserProfile | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

const ROLE_LABEL: Record<UserProfile['role'], string> = {
  LAWYER: 'Юрист',
  BUSINESS_USER: 'Бизнес-пользователь',
  ORG_ADMIN: 'Администратор',
};

export function OrgCard({ user, isLoading, error }: OrgCardProps): JSX.Element {
  return (
    <section
      aria-label="Организация"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header>
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">Организация</h2>
      </header>

      {isLoading && !user ? (
        <div className="flex min-h-[80px] items-center justify-center" aria-busy="true">
          <Spinner size="sm" aria-hidden="true" />
        </div>
      ) : error ? (
        <p role="alert" className="text-sm text-danger">
          Не удалось загрузить профиль организации.
        </p>
      ) : !user ? (
        <p className="text-sm text-fg-muted">Нет данных профиля.</p>
      ) : (
        <div className="flex flex-col gap-1">
          <p className="text-base font-semibold text-fg">{user.organization_name}</p>
          <p className="text-sm text-fg">{user.name}</p>
          <p className="text-sm text-fg-muted">{user.email}</p>
          <Badge variant="brand" className="mt-2 self-start">
            {ROLE_LABEL[user.role]}
          </Badge>
        </div>
      )}
    </section>
  );
}
