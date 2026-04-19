// RiskProfileDelta — три строки (high/medium/low) с визуализацией:
//   base.value → target.value   ±delta
//
// Цветовая схема (high/medium):
//   delta < 0 — зелёный (стало меньше → лучше)
//   delta > 0 — красный (больше → хуже)
//   delta = 0 — нейтральный
// Для low та же шкала: знак не критичен с точки зрения юзера, но
// визуально единообразно (меньше — лучше).
import { cn } from '@/shared/lib/cn';

import type { RiskProfileDeltaValue, RiskProfileSnapshot } from '../model/types';

export interface RiskProfileDeltaProps {
  delta: RiskProfileDeltaValue;
  baseProfile?: RiskProfileSnapshot;
  targetProfile?: RiskProfileSnapshot;
  className?: string;
}

interface RowDef {
  key: keyof RiskProfileDeltaValue;
  label: string;
  riskClass: string;
}

const ROWS: readonly RowDef[] = [
  { key: 'high', label: 'Высокие', riskClass: 'text-risk-high' },
  { key: 'medium', label: 'Средние', riskClass: 'text-risk-medium' },
  { key: 'low', label: 'Низкие', riskClass: 'text-risk-low' },
];

function deltaTextClass(value: number): string {
  if (value < 0) return 'text-success';
  if (value > 0) return 'text-danger';
  return 'text-fg-muted';
}

function formatDelta(value: number): string {
  if (value === 0) return '0';
  return value > 0 ? `+${value}` : String(value);
}

export function RiskProfileDelta({
  delta,
  baseProfile,
  targetProfile,
  className,
}: RiskProfileDeltaProps): JSX.Element {
  return (
    <section
      aria-label="Дельта профиля рисков"
      data-testid="risk-profile-delta"
      className={cn('flex flex-col gap-2 rounded-lg border border-border bg-bg p-4', className)}
    >
      <h3 className="text-sm font-semibold text-fg">Изменение профиля рисков</h3>
      <ul className="flex flex-col gap-1.5">
        {ROWS.map((row) => {
          const baseVal = baseProfile?.[row.key] ?? 0;
          const targetVal = targetProfile?.[row.key] ?? 0;
          const d = delta[row.key];
          return (
            <li
              key={row.key}
              data-testid={`risk-delta-row-${row.key}`}
              className="grid grid-cols-[1fr,auto,auto] items-baseline gap-3 text-sm"
            >
              <span className={cn('font-medium', row.riskClass)}>{row.label}</span>
              <span className="tabular-nums text-fg-muted">
                <span className="text-fg">{baseVal}</span>
                {' → '}
                <span className="text-fg">{targetVal}</span>
              </span>
              <span
                className={cn('font-mono text-xs tabular-nums', deltaTextClass(d))}
                data-testid={`risk-delta-value-${row.key}`}
              >
                {formatDelta(d)}
              </span>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
