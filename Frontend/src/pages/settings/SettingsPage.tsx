// SettingsPage (FE-TASK-049, §17.1, §17.3) — базовый экран настроек
// профиля пользователя. Auth-only (router.tsx), RBAC не требуется.
//
// 3 состояния:
//   Default — useMe загружен: 3 секции (Профиль / Организация / Сессия).
//   Loading — useMe.isLoading: центрированный Spinner (staleTime 60s +
//             pre-hydration на login делают этот путь краткосрочным).
//   Error   — useMe.error: role=alert + кнопка «Повторить» (refetch);
//             ErrorBoundary не используется — ошибка сети восстановима.
//
// i18n: в проекте один locale (ru, src/shared/i18n/config.ts) — language
// switcher из AC пропускаем «Опционально, если i18n содержит другие локали».
//
// ROLE_LABEL продублирован из widgets/dashboard-org-card/OrgCard.tsx. Для
// двух использований это дешевле, чем вводить shared/ui/role-badge. Если
// появится третий потребитель — вынести в shared.
import { useMe, type UserProfile } from '@/entities/user';
import { useLogout } from '@/features/auth/logout';
import { Badge, Button, Spinner } from '@/shared/ui';

const ROLE_LABEL: Record<UserProfile['role'], string> = {
  LAWYER: 'Юрист',
  BUSINESS_USER: 'Бизнес-пользователь',
  ORG_ADMIN: 'Администратор',
};

export function SettingsPage(): JSX.Element {
  const meQuery = useMe();
  const { logout, isPending } = useLogout();

  return (
    <main
      data-testid="page-settings"
      className="mx-auto flex w-full max-w-3xl flex-col gap-6 px-4 py-6 md:px-6 md:py-8"
    >
      <header>
        <h1 className="text-2xl font-semibold text-fg">Настройки</h1>
        <p className="mt-1 text-sm text-fg-muted">Профиль пользователя и текущая сессия.</p>
      </header>

      {meQuery.isLoading && !meQuery.data ? (
        <div
          data-testid="settings-loading"
          className="flex min-h-[160px] items-center justify-center rounded-md border border-border bg-bg p-5 shadow-sm"
          aria-busy="true"
          aria-live="polite"
        >
          <Spinner size="md" aria-hidden="true" />
          <span className="sr-only">Загрузка профиля…</span>
        </div>
      ) : meQuery.error && !meQuery.data ? (
        <section
          data-testid="settings-error"
          role="alert"
          className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
        >
          <p className="text-sm text-danger">Не удалось загрузить профиль пользователя.</p>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              void meQuery.refetch();
            }}
            loading={meQuery.isFetching}
            className="self-start"
          >
            Повторить
          </Button>
        </section>
      ) : meQuery.data ? (
        <ProfileSections user={meQuery.data} />
      ) : null}

      <section
        aria-label="Сессия"
        className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
      >
        <header>
          <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">Сессия</h2>
        </header>
        <p className="text-sm text-fg-muted">Завершите текущий сеанс на этом устройстве.</p>
        <Button
          data-testid="settings-logout-btn"
          variant="secondary"
          size="md"
          loading={isPending}
          disabled={isPending}
          onClick={() => {
            void logout();
          }}
          className="self-start"
        >
          Выйти
        </Button>
      </section>
    </main>
  );
}

function ProfileSections({ user }: { user: UserProfile }): JSX.Element {
  return (
    <>
      <section
        aria-label="Профиль"
        className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
      >
        <header>
          <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">Профиль</h2>
        </header>
        <dl className="grid grid-cols-1 gap-2 sm:grid-cols-[140px_1fr]">
          <dt className="text-sm text-fg-muted">Имя</dt>
          <dd className="text-sm text-fg">{user.name}</dd>
          <dt className="text-sm text-fg-muted">Email</dt>
          <dd className="text-sm text-fg">{user.email}</dd>
        </dl>
      </section>

      <section
        aria-label="Организация"
        className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
      >
        <header>
          <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
            Организация
          </h2>
        </header>
        <dl className="grid grid-cols-1 gap-2 sm:grid-cols-[140px_1fr]">
          <dt className="text-sm text-fg-muted">Название</dt>
          <dd className="text-sm text-fg">{user.organization_name}</dd>
          <dt className="text-sm text-fg-muted">Роль</dt>
          <dd>
            <Badge variant="brand">{ROLE_LABEL[user.role]}</Badge>
          </dd>
        </dl>
      </section>
    </>
  );
}
