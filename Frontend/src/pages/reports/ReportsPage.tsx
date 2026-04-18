/**
 * Placeholder для /reports (FE-TASK-031). Финальная имплементация — FE-TASK-048
 * (ReportsMetrics, ReportsTable, ReportDetailPanel, ShareModal; 10 состояний Figma).
 */
export function ReportsPage(): JSX.Element {
  return (
    <main
      data-testid="page-reports"
      className="mx-auto flex min-h-[60vh] max-w-5xl flex-col gap-3 px-6 py-12"
    >
      <h1 className="text-2xl font-semibold text-fg">Отчёты</h1>
      <p className="text-base text-fg-muted">
        Реестр отчётов появится в FE-TASK-048. Сейчас доступен только маршрут.
      </p>
    </main>
  );
}
