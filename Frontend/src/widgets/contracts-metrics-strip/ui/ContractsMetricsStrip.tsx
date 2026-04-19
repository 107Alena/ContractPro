// ContractsMetricsStrip (FE-TASK-044) — верхняя KPI-полоса на странице «Документы»
// (§17.4). Четыре счётчика по состоянию документа (ACTIVE/ARCHIVED/DELETED) +
// общий total. Отличается от Dashboard WhatMattersCards (там — агрегация по
// processing_status). Полностью презентационный виджет — данные приходят
// пропами от page.
//
// ВАЖНО: при серверной пагинации items[] = только текущая страница. Поэтому
// метки карточек active/archived/deleted содержат «на странице» — чтобы не
// вводить пользователя в заблуждение (считается по первым N строкам).
// Серверный total показывается честно. Когда backend добавит агрегаты по
// статусу — переключимся на глобальные счётчики.
import { useMemo } from 'react';

import { type ContractSummary } from '@/entities/contract';
import { Spinner } from '@/shared/ui';

export interface ContractsMetricsStripProps {
  items?: readonly ContractSummary[] | undefined;
  /** Серверный total (по всем страницам). Если не передан — считаем из items. */
  total?: number | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

interface StripCounters {
  total: number;
  active: number;
  archived: number;
  deleted: number;
}

export function computeStripCounters(
  items: readonly ContractSummary[],
  total: number,
): StripCounters {
  const c: StripCounters = { total, active: 0, archived: 0, deleted: 0 };
  for (const item of items) {
    switch (item.status) {
      case 'ACTIVE':
        c.active += 1;
        break;
      case 'ARCHIVED':
        c.archived += 1;
        break;
      case 'DELETED':
        c.deleted += 1;
        break;
      default:
        // status может быть undefined — не считаем.
        break;
    }
  }
  return c;
}

export function ContractsMetricsStrip({
  items,
  total,
  isLoading,
  error,
}: ContractsMetricsStripProps): JSX.Element {
  const counters = useMemo<StripCounters>(() => {
    const safeItems = items ?? [];
    const safeTotal = typeof total === 'number' ? total : safeItems.length;
    return computeStripCounters(safeItems, safeTotal);
  }, [items, total]);

  if (isLoading && !items) {
    return (
      <section
        aria-label="Показатели договоров"
        aria-busy="true"
        className="flex h-[120px] items-center justify-center rounded-md border border-border bg-bg-muted"
        data-testid="contracts-metrics-strip-loading"
      >
        <Spinner size="md" aria-hidden="true" />
      </section>
    );
  }

  if (error) {
    return (
      <section
        aria-label="Показатели договоров"
        role="alert"
        className="rounded-md border border-danger/30 bg-[color-mix(in_srgb,var(--color-danger)_8%,transparent)] p-4 text-sm text-danger"
        data-testid="contracts-metrics-strip-error"
      >
        Не удалось загрузить показатели. Попробуйте обновить страницу.
      </section>
    );
  }

  const cards: Array<{ key: keyof StripCounters; label: string; tone: string }> = [
    { key: 'total', label: 'Всего договоров', tone: 'text-fg' },
    { key: 'active', label: 'Активных на странице', tone: 'text-brand-600' },
    { key: 'archived', label: 'В архиве (на странице)', tone: 'text-fg-muted' },
    { key: 'deleted', label: 'Удалённых (на странице)', tone: 'text-danger' },
  ];

  return (
    <section
      aria-label="Показатели договоров"
      className="grid grid-cols-2 gap-3 md:grid-cols-4"
      data-testid="contracts-metrics-strip"
    >
      {cards.map((card) => (
        <article
          key={card.key}
          className="rounded-md border border-border bg-bg p-4 shadow-sm"
          data-testid={`contracts-metrics-strip-card-${card.key}`}
        >
          <p className="text-xs font-medium uppercase tracking-wide text-fg-muted">{card.label}</p>
          <p className={`mt-2 text-3xl font-semibold ${card.tone}`}>{counters[card.key]}</p>
        </article>
      ))}
    </section>
  );
}
