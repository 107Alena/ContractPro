import { useParams } from 'react-router-dom';

/**
 * Placeholder для /contracts/:id (FE-TASK-031). Финальная имплементация — FE-TASK-045
 * (DocumentHeader, SummaryCard, KeyRisks, Recommendations, VersionsTimeline, PDFNavigator
 * lazy-loaded в chunks/pdf-preview).
 */
export function ContractDetailPage(): JSX.Element {
  const { id } = useParams<{ id: string }>();
  return (
    <main
      data-testid="page-contract-detail"
      className="mx-auto flex min-h-[60vh] max-w-5xl flex-col gap-3 px-6 py-12"
    >
      <h1 className="text-2xl font-semibold text-fg">Договор</h1>
      <p className="text-sm text-fg-muted">ID: {id ?? '—'}</p>
      <p className="text-base text-fg-muted">
        Карточка договора появится в FE-TASK-045. Сейчас доступен только маршрут.
      </p>
    </main>
  );
}
