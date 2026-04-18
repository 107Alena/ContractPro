import { useParams } from 'react-router-dom';

/**
 * Placeholder для /contracts/:id/versions/:vid/result (FE-TASK-031). Финальная имплементация —
 * FE-TASK-046 (DocumentCard, RiskProfileCard, MandatoryConditions, RisksList, SummaryTable,
 * Recommendations, FeedbackBlock; 8 состояний Figma).
 */
export function ResultPage(): JSX.Element {
  const { id, vid } = useParams<{ id: string; vid: string }>();
  return (
    <main
      data-testid="page-result"
      className="mx-auto flex min-h-[60vh] max-w-5xl flex-col gap-3 px-6 py-12"
    >
      <h1 className="text-2xl font-semibold text-fg">Результат проверки</h1>
      <p className="text-sm text-fg-muted">
        Договор: {id ?? '—'} · Версия: {vid ?? '—'}
      </p>
      <p className="text-base text-fg-muted">
        Полный результат проверки появится в FE-TASK-046. Сейчас доступен только маршрут.
      </p>
    </main>
  );
}
