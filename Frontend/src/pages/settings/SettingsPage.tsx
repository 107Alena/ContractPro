/**
 * Placeholder для /settings (FE-TASK-031). Финальная имплементация — FE-TASK-049
 * (профиль пользователя + кнопка «Выйти»).
 */
export function SettingsPage(): JSX.Element {
  return (
    <main
      data-testid="page-settings"
      className="mx-auto flex min-h-[60vh] max-w-3xl flex-col gap-3 px-6 py-12"
    >
      <h1 className="text-2xl font-semibold text-fg">Настройки</h1>
      <p className="text-base text-fg-muted">
        Настройки профиля появятся в FE-TASK-049. Сейчас доступен только маршрут.
      </p>
    </main>
  );
}
