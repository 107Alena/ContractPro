// ComparisonVerdictCard — крупная карточка с итоговой оценкой:
// «Лучше / Хуже / Смешанные изменения / Без изменений», обоснование
// и сравнение high+medium профилей.
import { cn } from '@/shared/lib/cn';
import { Badge, type BadgeProps } from '@/shared/ui/badge';

import type { ComparisonVerdict, RiskProfileSnapshot } from '../model/types';

export interface ComparisonVerdictCardProps {
  verdict: ComparisonVerdict;
  baseProfile?: RiskProfileSnapshot;
  targetProfile?: RiskProfileSnapshot;
  className?: string;
}

interface VerdictView {
  label: string;
  description: string;
  variant: NonNullable<BadgeProps['variant']>;
}

const VERDICT_VIEW: Record<ComparisonVerdict, VerdictView> = {
  better: {
    label: 'Лучше',
    description:
      'Профиль рисков целевой версии улучшился: критичных и средних рисков стало меньше.',
    variant: 'success',
  },
  worse: {
    label: 'Хуже',
    description:
      'Профиль рисков целевой версии ухудшился: критичных и средних рисков стало больше.',
    variant: 'danger',
  },
  mixed: {
    label: 'Смешанные изменения',
    description: 'Часть рисков снизилась, часть — выросла. Требуется внимательная проверка.',
    variant: 'warning',
  },
  unchanged: {
    label: 'Без изменений',
    description: 'Профиль рисков целевой версии не изменился относительно базовой.',
    variant: 'neutral',
  },
};

function severitySum(p?: RiskProfileSnapshot): number {
  return p ? p.high + p.medium : 0;
}

export function ComparisonVerdictCard({
  verdict,
  baseProfile,
  targetProfile,
  className,
}: ComparisonVerdictCardProps): JSX.Element {
  const view = VERDICT_VIEW[verdict];
  const baseSum = severitySum(baseProfile);
  const targetSum = severitySum(targetProfile);
  const showSummary = baseProfile !== undefined || targetProfile !== undefined;

  return (
    <section
      aria-label="Итоговая оценка изменений"
      data-testid="comparison-verdict-card"
      className={cn('flex flex-col gap-3 rounded-lg border border-border bg-bg p-5', className)}
    >
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="text-lg font-semibold text-fg">Итоговая оценка</h2>
        <Badge variant={view.variant} data-testid="comparison-verdict-badge">
          {view.label}
        </Badge>
      </div>
      <p className="text-sm text-fg-muted">{view.description}</p>
      {showSummary ? (
        <p className="text-sm text-fg" data-testid="comparison-verdict-summary">
          Высокие + средние риски: <span className="font-medium">{baseSum}</span>
          {' → '}
          <span className="font-medium">{targetSum}</span>
        </p>
      ) : null}
    </section>
  );
}
