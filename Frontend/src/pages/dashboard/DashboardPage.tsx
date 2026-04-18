/**
 * Placeholder для /dashboard (FE-TASK-031). Финальная имплементация — FE-TASK-042
 * (WhatMattersCards, LastCheckCard, QuickStart, OrgCard, RecentChecksTable, KeyRisksCards + SSE).
 */
export function DashboardPage(): JSX.Element {
  return (
    <main
      data-testid="page-dashboard"
      className="mx-auto flex min-h-[60vh] max-w-5xl flex-col gap-3 px-6 py-12"
    >
      <h1 className="text-2xl font-semibold text-fg">Главная</h1>
      <p className="text-base text-fg-muted">
        Дашборд появится в FE-TASK-042. Сейчас доступен только маршрут.
      </p>
    </main>
  );
}
