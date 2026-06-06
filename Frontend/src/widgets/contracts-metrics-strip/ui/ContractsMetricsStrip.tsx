// ContractsMetricsStrip (FE-TASK-044/058, Figma 198:2) — сводка над списком.
//
// data-honesty:
//   • «документов» = серверный total (глобальный, реальный);
//   • «в обработке» / «высокий риск» / «требуют внимания» считаются из items
//     ТЕКУЩЕЙ СТРАНИЦЫ (глобального aggregate-эндпоинта нет — показываем срез
//     загруженной страницы). Охват подписан видимо под полосой + sr-only на каждой
//     метрике, чтобы число не читалось как глобальное;
//   • «высокий риск» с ORCH-TASK-056 считается из реального risk_level (page-scoped);
//   • «завершено сегодня» из Figma исключена: требует глобального date-aggregate
//     (вне scope), честно посчитать из текущей страницы нельзя. Никаких выдуманных чисел.
import { useMemo } from 'react';

import { type ContractSummary, viewStatus } from '@/entities/contract';
import { Card, Spinner } from '@/shared/ui';

export interface ContractsMetricsStripProps {
  items?: readonly ContractSummary[] | undefined;
  /** Серверный total (по всем страницам). Если не передан — считаем из items. */
  total?: number | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

export interface StripCounts {
  inProgress: number;
  attention: number;
  highRisk: number;
}

export function computeStripCounters(items: readonly ContractSummary[]): StripCounts {
  const c: StripCounts = { inProgress: 0, attention: 0, highRisk: 0 };
  for (const item of items) {
    const { bucket } = viewStatus(item.processing_status);
    if (bucket === 'in_progress') c.inProgress += 1;
    else if (bucket === 'awaiting' || bucket === 'failed') c.attention += 1;
    if (item.risk_level === 'high') c.highRisk += 1;
  }
  return c;
}

export function ContractsMetricsStrip({
  items,
  total,
  isLoading,
  error,
}: ContractsMetricsStripProps): JSX.Element {
  const counts = useMemo(() => computeStripCounters(items ?? []), [items]);

  if (isLoading && !items) {
    return (
      <Card
        aria-label="Показатели договоров"
        aria-busy="true"
        className="flex h-[88px] items-center justify-center"
        data-testid="contracts-metrics-strip-loading"
      >
        <Spinner size="md" aria-hidden="true" />
        <span className="sr-only">Загрузка…</span>
      </Card>
    );
  }

  if (error) {
    return (
      <Card
        aria-label="Показатели договоров"
        className="p-5"
        data-testid="contracts-metrics-strip-error"
      >
        <p role="alert" className="text-14 text-danger">
          Не удалось загрузить показатели. Попробуйте обновить страницу.
        </p>
      </Card>
    );
  }

  const safeTotal = typeof total === 'number' ? total : (items?.length ?? 0);

  const stats: Array<{
    key: string;
    value: number | string;
    label: string;
    dot?: string;
    muted?: boolean;
    /** Счётчик по текущей странице (не глобальный) — добавляем пояснение об охвате. */
    scoped?: boolean;
  }> = [
    { key: 'total', value: safeTotal, label: 'документов' },
    {
      key: 'in-progress',
      value: counts.inProgress,
      label: 'в обработке',
      dot: 'bg-processing',
      scoped: true,
    },
    {
      key: 'high-risk',
      value: counts.highRisk,
      label: 'высокий риск',
      dot: 'bg-risk-high',
      scoped: true,
    },
    {
      key: 'attention',
      value: counts.attention,
      label: 'требуют внимания',
      dot: 'bg-warning',
      scoped: true,
    },
  ];

  return (
    <Card
      aria-label="Показатели договоров"
      className="flex flex-col gap-2 px-5 py-4"
      data-testid="contracts-metrics-strip"
    >
      <div className="flex flex-wrap items-center gap-x-8 gap-y-4">
        {stats.map((s) => (
          <div
            key={s.key}
            className="flex items-center gap-2"
            data-testid={`contracts-metrics-strip-card-${s.key}`}
            {...(s.scoped ? { title: `${s.label} — на текущей странице` } : {})}
          >
            {s.dot ? (
              <span className={`size-2 shrink-0 rounded-full ${s.dot}`} aria-hidden="true" />
            ) : null}
            <span className={`text-24 font-bold ${s.muted ? 'text-fg-disabled' : 'text-fg'}`}>
              {s.value}
            </span>
            <span className="text-13 text-fg-muted">
              {s.label}
              {s.scoped ? <span className="sr-only"> — на текущей странице</span> : null}
            </span>
          </div>
        ))}
      </div>
      {/* Видимая отметка охвата: «в обработке»/«высокий риск»/«требуют внимания»
          считаются по загруженной странице, «документов» — глобально (data-honesty,
          число не должно читаться как all-time). */}
      <p className="text-12 text-fg-subtle" data-testid="contracts-metrics-strip-scope-note">
        В обработке, высокий риск и требуют внимания — по текущей странице.
      </p>
    </Card>
  );
}
