/**
 * Placeholder для /login (FE-TASK-031). Финальная имплементация — FE-TASK-029
 * (React Hook Form + Zod + applyValidationErrors + redirect ?next=...).
 */
export function LoginPage(): JSX.Element {
  return (
    <main
      data-testid="page-login"
      className="mx-auto flex min-h-[60vh] max-w-md flex-col items-center justify-center gap-3 px-6 py-16 text-center"
    >
      <h1 className="text-2xl font-semibold text-fg">Вход</h1>
      <p className="text-base text-fg-muted">
        Форма входа появится в FE-TASK-029. Сейчас доступен только маршрут.
      </p>
    </main>
  );
}
