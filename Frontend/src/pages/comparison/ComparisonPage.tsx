import { useParams, useSearchParams } from 'react-router-dom';

/**
 * Placeholder для /contracts/:id/compare?base=&target= (FE-TASK-031). Финальная имплементация —
 * FE-TASK-047 (DiffViewer lazy-loaded в chunks/diff-viewer; 9 состояний Figma).
 */
export function ComparisonPage(): JSX.Element {
  const { id } = useParams<{ id: string }>();
  const [searchParams] = useSearchParams();
  const base = searchParams.get('base');
  const target = searchParams.get('target');
  return (
    <main
      data-testid="page-comparison"
      className="mx-auto flex min-h-[60vh] max-w-6xl flex-col gap-3 px-6 py-12"
    >
      <h1 className="text-2xl font-semibold text-fg">Сравнение версий</h1>
      <p className="text-sm text-fg-muted">
        Договор: {id ?? '—'} · База: {base ?? '—'} · Целевая: {target ?? '—'}
      </p>
      <p className="text-base text-fg-muted">
        Полный DiffViewer появится в FE-TASK-047. Сейчас доступен только маршрут.
      </p>
    </main>
  );
}
