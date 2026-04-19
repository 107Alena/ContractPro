// ChangeCounters — сетка карточек-плашек: Всего / Добавлено / Удалено / Изменено.
// Под сеткой — строка «Из них: текстовых X / структурных Y».
import { cn } from '@/shared/lib/cn';

import type { ChangeCountersValue } from '../model/types';

export interface ChangeCountersProps {
  counters: ChangeCountersValue;
  className?: string;
}

interface PlateProps {
  label: string;
  value: number;
  testId: string;
  accent?: 'neutral' | 'success' | 'danger' | 'warning';
}

const ACCENT_TEXT: Record<NonNullable<PlateProps['accent']>, string> = {
  neutral: 'text-fg',
  success: 'text-success',
  danger: 'text-danger',
  warning: 'text-warning',
};

function Plate({ label, value, testId, accent = 'neutral' }: PlateProps): JSX.Element {
  return (
    <div
      data-testid={testId}
      className="flex flex-col gap-1 rounded-md border border-border bg-bg-muted px-3 py-2"
    >
      <span className="text-xs font-medium uppercase tracking-wide text-fg-muted">{label}</span>
      <span className={cn('text-2xl font-semibold tabular-nums', ACCENT_TEXT[accent])}>
        {value}
      </span>
    </div>
  );
}

export function ChangeCounters({ counters, className }: ChangeCountersProps): JSX.Element {
  return (
    <section
      aria-label="Счётчики изменений"
      data-testid="change-counters"
      className={cn('flex flex-col gap-2', className)}
    >
      <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
        <Plate label="Всего" value={counters.total} testId="counter-total" accent="neutral" />
        <Plate label="Добавлено" value={counters.added} testId="counter-added" accent="success" />
        <Plate label="Удалено" value={counters.removed} testId="counter-removed" accent="danger" />
        <Plate
          label="Изменено"
          value={counters.modified + counters.moved}
          testId="counter-modified"
          accent="warning"
        />
      </div>
      <p className="text-xs text-fg-muted" data-testid="counter-breakdown">
        Из них: текстовых <span className="font-medium text-fg">{counters.textual}</span>
        {' / структурных '}
        <span className="font-medium text-fg">{counters.structural}</span>
      </p>
    </section>
  );
}
