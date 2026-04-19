import { forwardRef, type HTMLAttributes, type ReactNode } from 'react';

import { Badge, SimpleTooltip } from '@/shared/ui';

import { RISK_LEVEL_META, type RiskLevel } from '../model';

// RiskBadge — уровень риска (high/medium/low) в виде цветного лейбла.
//
// Per spec (tasks.json FE-TASK-024): «tooltip с legend». Default
// `showTooltip={false}` — в таблицах (RisksList, DocumentsTable, Reports)
// hover-per-row создаёт шум и нагружает a11y focus-trap. Потребители в
// контексте легенды/заголовков (RiskProfileCard, LegalDisclaimer) явно
// включают tooltip через `showTooltip`.

export interface RiskBadgeProps extends Omit<HTMLAttributes<HTMLSpanElement>, 'children'> {
  level: RiskLevel;
  /** Показать tooltip с legend (ТЗ-1 §4.3). По умолчанию `false` — см. header. */
  showTooltip?: boolean;
  /** Переопределить содержимое лейбла (по умолчанию — локализованный label из meta). */
  children?: ReactNode;
}

export const RiskBadge = forwardRef<HTMLSpanElement, RiskBadgeProps>(function RiskBadge(
  { level, showTooltip = false, className, children, ...rest },
  ref,
) {
  const meta = RISK_LEVEL_META[level];
  const badge = (
    <Badge
      ref={ref}
      className={className}
      {...rest}
      variant={meta.tone}
      data-testid="risk-badge"
      data-level={level}
    >
      {children ?? meta.label}
    </Badge>
  );
  if (!showTooltip) return badge;
  return (
    <SimpleTooltip content={meta.legend} size="md">
      {badge}
    </SimpleTooltip>
  );
});
