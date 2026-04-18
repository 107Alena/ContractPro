/**
 * Placeholder для /contracts (FE-TASK-031). Финальная имплементация — FE-TASK-044
 * (MetricsStrip, WhatMattersCards, SearchBar, FilterChips, DocumentsTable c виртуализацией).
 */
export function ContractsListPage(): JSX.Element {
  return (
    <main
      data-testid="page-contracts-list"
      className="mx-auto flex min-h-[60vh] max-w-6xl flex-col gap-3 px-6 py-12"
    >
      <h1 className="text-2xl font-semibold text-fg">Документы</h1>
      <p className="text-base text-fg-muted">
        Список договоров появится в FE-TASK-044. Сейчас доступен только маршрут.
      </p>
    </main>
  );
}
