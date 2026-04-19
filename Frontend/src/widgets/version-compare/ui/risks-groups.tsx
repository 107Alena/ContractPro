// RisksGroups — три аккордеона native <details>:
//   «Исчезнувшие» / «Новые» / «Без изменений».
// RiskBadge — Badge variant=danger/warning/neutral по уровню риска.
import { cn } from '@/shared/lib/cn';
import { Badge, type BadgeProps } from '@/shared/ui/badge';

import type { ComparisonRiskItem, ComparisonRisksGroups } from '../model/types';

export interface RisksGroupsProps {
  groups: ComparisonRisksGroups;
  className?: string;
}

const LEVEL_VARIANT: Record<ComparisonRiskItem['level'], NonNullable<BadgeProps['variant']>> = {
  high: 'danger',
  medium: 'warning',
  low: 'neutral',
};

const LEVEL_LABEL: Record<ComparisonRiskItem['level'], string> = {
  high: 'Высокий',
  medium: 'Средний',
  low: 'Низкий',
};

interface GroupSectionProps {
  title: string;
  items: readonly ComparisonRiskItem[];
  testId: string;
  /** Раскрыт ли по умолчанию. Resolved/introduced — раскрыты, unchanged — нет. */
  defaultOpen?: boolean;
}

function GroupSection({
  title,
  items,
  testId,
  defaultOpen = false,
}: GroupSectionProps): JSX.Element {
  return (
    <details
      open={defaultOpen}
      data-testid={testId}
      className="group rounded-md border border-border bg-bg"
    >
      <summary className="flex cursor-pointer items-center justify-between gap-2 px-3 py-2 text-sm font-medium text-fg">
        <span>
          {title}{' '}
          <span className="text-fg-muted" data-testid={`${testId}-count`}>
            ({items.length})
          </span>
        </span>
        <span aria-hidden className="text-fg-muted transition-transform group-open:rotate-180">
          ▾
        </span>
      </summary>
      {items.length === 0 ? (
        <p className="px-3 pb-3 text-sm text-fg-muted">Список пуст.</p>
      ) : (
        <ul className="flex flex-col gap-1 px-3 pb-3" role="group" aria-label={title}>
          {items.map((item) => (
            <li
              key={item.id}
              data-testid={`${testId}-item`}
              className="flex flex-wrap items-center justify-between gap-2 rounded-sm border border-border px-2 py-1.5"
            >
              <span className="text-sm text-fg">{item.title}</span>
              <span className="flex items-center gap-1.5">
                {item.category ? (
                  <span className="text-xs text-fg-muted">{item.category}</span>
                ) : null}
                <Badge variant={LEVEL_VARIANT[item.level]}>{LEVEL_LABEL[item.level]}</Badge>
              </span>
            </li>
          ))}
        </ul>
      )}
    </details>
  );
}

export function RisksGroups({ groups, className }: RisksGroupsProps): JSX.Element {
  return (
    <section
      aria-label="Сравнение рисков по версиям"
      data-testid="risks-groups"
      className={cn('flex flex-col gap-2', className)}
    >
      <GroupSection
        title="Исчезнувшие риски"
        items={groups.resolved}
        testId="risks-groups-resolved"
        defaultOpen
      />
      <GroupSection
        title="Новые риски"
        items={groups.introduced}
        testId="risks-groups-introduced"
        defaultOpen
      />
      <GroupSection
        title="Без изменений"
        items={groups.unchanged}
        testId="risks-groups-unchanged"
      />
    </section>
  );
}
