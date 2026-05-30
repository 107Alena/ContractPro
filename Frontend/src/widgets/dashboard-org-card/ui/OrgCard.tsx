// OrgCard — карточка «Организация» на dashboard (Figma 84:2 → 92:38).
//
// Строки Политика / Пользователей / Чек-листы требуют данных OPM (лимиты,
// чек-листы), которых нет в /users/me → структурный «—». Реальна только роль
// пользователя. Название организации показывается в сайдбаре (WorkspaceSwitcher),
// здесь не дублируется — соответствует Figma.
import { type ReactNode } from 'react';
import { Link } from 'react-router-dom';

import { type UserProfile } from '@/entities/user';
import { Card, Spinner } from '@/shared/ui';

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
    <Card as="article" aria-label="Организация" className="flex flex-col gap-2.5 p-5">
      <h2 className="text-15 font-semibold text-fg">Организация</h2>

      {isLoading && !user ? (
        <div className="flex min-h-[60px] items-center justify-center" aria-busy="true">
          <Spinner size="sm" aria-hidden="true" />
          <span className="sr-only">Загрузка…</span>
        </div>
      ) : error ? (
        <p role="alert" className="text-14 text-danger">
          Не удалось загрузить профиль организации.
        </p>
      ) : !user ? (
        <p className="text-13 text-fg-muted">Нет данных профиля.</p>
      ) : (
        <>
          <Row label="Политика" value={<Dash />} />
          <Row label="Пользователей" value={<Dash />} />
          <Row label="Ваша роль" value={ROLE_LABEL[user.role]} />
          <Row label="Чек-листы" value={<Dash />} />
          <Link
            to="/settings"
            className="text-13 font-medium text-brand-600 hover:text-brand-500 focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2"
          >
            Перейти в настройки →
          </Link>
        </>
      )}
    </Card>
  );
}

function Row({ label, value }: { label: string; value: ReactNode }): JSX.Element {
  return (
    <div className="flex items-center justify-between gap-2 text-13">
      <span className="text-fg-subtle">{label}</span>
      <span className="font-medium text-fg-strong">{value}</span>
    </div>
  );
}

function Dash(): JSX.Element {
  return <span className="text-fg-disabled">—</span>;
}
