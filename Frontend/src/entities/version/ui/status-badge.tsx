import { forwardRef, type HTMLAttributes } from 'react';

import { statusMeta } from '@/shared/lib/status-view';
import { Badge } from '@/shared/ui';

import type { VersionStatus } from '../model';

// StatusBadge — единый presentational-примитив для `VersionStatus`
// (10 значений UserProcessingStatus, §8.3 high-architecture.md). Маппинг
// label+tone — в `shared/lib/status-view` (единый источник истины для этого
// виджета и `entities/contract/model/status-view.ts`).
//
// Использование: `<StatusBadge status={version.processing_status} />` в
// таблицах, карточках версий и dashboard-виджетах.

export interface StatusBadgeProps extends HTMLAttributes<HTMLSpanElement> {
  /** Статус версии (UserProcessingStatus). `undefined` → neutral «Неизвестно». */
  status: VersionStatus | undefined | null;
}

export const StatusBadge = forwardRef<HTMLSpanElement, StatusBadgeProps>(function StatusBadge(
  { status, className, children, ...rest },
  ref,
) {
  const { label, tone } = statusMeta(status);
  return (
    <Badge
      ref={ref}
      className={className}
      {...rest}
      variant={tone}
      data-testid="status-badge"
      data-status={status ?? 'UNKNOWN'}
    >
      {children ?? label}
    </Badge>
  );
});
