// WhatMattersCards — блок KPI для главного экрана (§17.4 «Dashboard»).
//
// Агрегирует список последних договоров из /contracts?size=5 и рендерит
// 4 счётчика: всего, в работе, готовы, проблемные. Презентационный компонент —
// данные приходят пропами от DashboardPage.
import { type ContractSummary, viewStatus } from '@/entities/contract';
import { Spinner } from '@/shared/ui';

export interface WhatMattersCardsProps {
  items?: readonly ContractSummary[] | undefined;
  total?: number | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

interface Counters {
  total: number;
  inProgress: number;
  ready: number;
  failed: number;
}

function computeCounters(items: readonly ContractSummary[], total: number): Counters {
  const counters: Counters = { total, inProgress: 0, ready: 0, failed: 0 };
  for (const item of items) {
    const { bucket } = viewStatus(item.processing_status);
    if (bucket === 'in_progress' || bucket === 'awaiting') counters.inProgress += 1;
    else if (bucket === 'ready') counters.ready += 1;
    else if (bucket === 'failed') counters.failed += 1;
  }
  return counters;
}

export function WhatMattersCards({
  items,
  total,
  isLoading,
  error,
}: WhatMattersCardsProps): JSX.Element {
  if (isLoading && !items) {
    return (
      <section
        aria-label="Ключевые показатели"
        aria-busy="true"
        className="flex h-[120px] items-center justify-center rounded-md border border-border bg-bg-muted"
      >
        <Spinner size="md" aria-hidden="true" />
      </section>
    );
  }

  if (error) {
    return (
      <section
        aria-label="Ключевые показатели"
        role="alert"
        className="rounded-md border border-danger/30 bg-[color-mix(in_srgb,var(--color-danger)_8%,transparent)] p-4 text-sm text-danger"
      >
        Не удалось загрузить показатели. Попробуйте обновить страницу.
      </section>
    );
  }

  const safeItems = items ?? [];
  const safeTotal = typeof total === 'number' ? total : safeItems.length;
  const counters = computeCounters(safeItems, safeTotal);

  const cards: Array<{ key: keyof Counters; label: string; tone: string }> = [
    { key: 'total', label: 'Всего проверок', tone: 'text-fg' },
    { key: 'inProgress', label: 'В работе', tone: 'text-brand-600' },
    { key: 'ready', label: 'Готовы', tone: 'text-success' },
    { key: 'failed', label: 'Требуют внимания', tone: 'text-danger' },
  ];

  return (
    <section aria-label="Ключевые показатели" className="grid grid-cols-2 gap-3 md:grid-cols-4">
      {cards.map((card) => (
        <article key={card.key} className="rounded-md border border-border bg-bg p-4 shadow-sm">
          <p className="text-xs font-medium uppercase tracking-wide text-fg-muted">{card.label}</p>
          <p className={`mt-2 text-3xl font-semibold ${card.tone}`}>{counters[card.key]}</p>
        </article>
      ))}
    </section>
  );
}

export { computeCounters };
