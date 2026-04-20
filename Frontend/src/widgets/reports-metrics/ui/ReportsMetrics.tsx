// ReportsMetrics (FE-TASK-048) — KPI-полоса экрана «Отчёты» (Figma 9, §17.4).
//
// 4 карточки:
//   - Всего отчётов (total c сервера — ACTIVE + любой processing_status)
//   - Готовые (items с processing_status=READY, на странице)
//   - С предупреждениями (PARTIALLY_FAILED, на странице)
//   - За 7 дней (updated_at > now-7d, на странице)
//
// Контракт `items`: серверная страница ДО клиентской фильтрации (see page
// использует `rawItems`). Это даёт честные счётчики по processing_status
// независимо от выбранного фильтра. Loading/Error по паттерну
// ContractsMetricsStrip — skeleton-секция / inline alert.
import { useMemo } from 'react';

import { type ContractSummary } from '@/entities/contract';
import { Spinner } from '@/shared/ui';

const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000;

export interface ReportsMetricsProps {
  items?: readonly ContractSummary[] | undefined;
  total?: number | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
  /** @internal test override для детерминированной проверки «За 7 дней». */
  now?: number;
}

export interface ReportsMetricsCounters {
  total: number;
  ready: number;
  partial: number;
  recent: number;
}

export function computeReportsCounters(
  items: readonly ContractSummary[],
  total: number,
  now: number = Date.now(),
): ReportsMetricsCounters {
  const c: ReportsMetricsCounters = { total, ready: 0, partial: 0, recent: 0 };
  for (const item of items) {
    if (item.processing_status === 'READY') c.ready += 1;
    if (item.processing_status === 'PARTIALLY_FAILED') c.partial += 1;
    const iso = item.updated_at ?? item.created_at;
    if (iso) {
      const t = Date.parse(iso);
      if (Number.isFinite(t) && now - t <= SEVEN_DAYS_MS) c.recent += 1;
    }
  }
  return c;
}

export function ReportsMetrics({
  items,
  total,
  isLoading,
  error,
  now,
}: ReportsMetricsProps): JSX.Element {
  const counters = useMemo<ReportsMetricsCounters>(() => {
    const safeItems = items ?? [];
    const safeTotal = typeof total === 'number' ? total : safeItems.length;
    return computeReportsCounters(safeItems, safeTotal, now);
  }, [items, total, now]);

  if (isLoading && !items) {
    return (
      <section
        aria-label="Показатели отчётов"
        aria-busy="true"
        className="flex h-[120px] items-center justify-center rounded-md border border-border bg-bg-muted"
        data-testid="reports-metrics-loading"
      >
        <Spinner size="md" aria-hidden="true" />
      </section>
    );
  }

  if (error) {
    return (
      <section
        aria-label="Показатели отчётов"
        role="alert"
        className="rounded-md border border-danger/30 bg-[color-mix(in_srgb,var(--color-danger)_8%,transparent)] p-4 text-sm text-danger"
        data-testid="reports-metrics-error"
      >
        Не удалось загрузить показатели. Попробуйте обновить страницу.
      </section>
    );
  }

  const cards: Array<{
    key: keyof ReportsMetricsCounters;
    label: string;
    tone: string;
    hint?: string;
  }> = [
    { key: 'total', label: 'Всего отчётов', tone: 'text-fg' },
    { key: 'ready', label: 'Готовых (на странице)', tone: 'text-brand-600' },
    { key: 'partial', label: 'С предупреждениями', tone: 'text-warning' },
    { key: 'recent', label: 'За 7 дней', tone: 'text-fg' },
  ];

  return (
    <section
      aria-label="Показатели отчётов"
      className="grid grid-cols-2 gap-3 md:grid-cols-4"
      data-testid="reports-metrics"
    >
      {cards.map((card) => (
        <article
          key={card.key}
          className="rounded-md border border-border bg-bg p-4 shadow-sm"
          data-testid={`reports-metrics-card-${card.key}`}
        >
          <p className="text-xs font-medium uppercase tracking-wide text-fg-muted">{card.label}</p>
          <p className={`mt-2 text-3xl font-semibold ${card.tone}`}>{counters[card.key]}</p>
        </article>
      ))}
    </section>
  );
}
