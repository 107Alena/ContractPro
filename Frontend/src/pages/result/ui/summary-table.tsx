// SummaryTable — композит «Резюме + ключевые параметры» (§16.5 дерево
// ResultPage, §17.5 artifacts SUMMARY + KEY_PARAMETERS rows 11/7).
// Доступен всем аутентифицированным — ключевой виджет для BUSINESS_USER
// (R-2 из ТЗ). Не требует <Can>-гейта.
import type { AnalysisResults } from '@/entities/result';
import type { components } from '@/shared/api/openapi';

type KeyParameters = components['schemas']['KeyParameters'];

export interface SummaryTableProps {
  results: AnalysisResults;
}

interface ParameterRow {
  label: string;
  value: string | null | undefined;
}

function buildRows(params: KeyParameters | undefined): readonly ParameterRow[] {
  if (!params) return [];
  return [
    {
      label: 'Стороны',
      value: params.parties && params.parties.length > 0 ? params.parties.join(', ') : null,
    },
    { label: 'Предмет договора', value: params.subject },
    { label: 'Цена', value: params.price },
    { label: 'Срок', value: params.duration },
    { label: 'Ответственность', value: params.penalties },
    { label: 'Юрисдикция', value: params.jurisdiction },
  ];
}

export function SummaryTable({ results }: SummaryTableProps): JSX.Element {
  const rows = buildRows(results.key_parameters);
  const filled = rows.filter((row) => Boolean(row.value));

  return (
    <section
      aria-label="Краткое резюме"
      data-testid="summary-table"
      className="flex flex-col gap-4 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header className="flex flex-col gap-1">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Краткое резюме
        </h2>
        <p className="text-xs text-fg-muted">
          Обзор договора для бизнес-пользователя: суть сделки и ключевые параметры
        </p>
      </header>

      {results.summary ? (
        <p className="text-base text-fg" data-testid="summary-table-text">
          {results.summary}
        </p>
      ) : (
        <p className="text-sm text-fg-muted" data-testid="summary-table-empty">
          Резюме появится после завершения анализа.
        </p>
      )}

      {filled.length > 0 ? (
        <dl className="grid grid-cols-1 gap-3 md:grid-cols-2" data-testid="summary-table-params">
          {filled.map((row) => (
            <div
              key={row.label}
              className="flex flex-col gap-1 rounded-md border border-border bg-bg-muted p-3"
            >
              <dt className="text-xs font-medium uppercase tracking-wide text-fg-muted">
                {row.label}
              </dt>
              <dd className="text-sm text-fg">{row.value}</dd>
            </div>
          ))}
        </dl>
      ) : null}
    </section>
  );
}
