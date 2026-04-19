// TabsFilters — переключатель фильтра ChangesTable. Tablist из 4 кнопок:
// Все / Текстовые / Структурные / Высокие риски. Поддерживает arrow-навигацию
// (Left/Right) — стандартный WAI-ARIA tablist-паттерн.
import { type KeyboardEvent, useCallback, useRef } from 'react';

import { cn } from '@/shared/lib/cn';

import type { ChangesFilter } from '../model/types';

export interface TabsFiltersProps {
  value: ChangesFilter;
  onChange: (next: ChangesFilter) => void;
  counters?: Partial<Record<ChangesFilter, number>>;
  className?: string;
}

interface TabDef {
  value: ChangesFilter;
  label: string;
}

const TABS: readonly TabDef[] = [
  { value: 'all', label: 'Все' },
  { value: 'textual', label: 'Текстовые' },
  { value: 'structural', label: 'Структурные' },
  { value: 'high-risk', label: 'Высокие риски' },
];

export function TabsFilters({
  value,
  onChange,
  counters,
  className,
}: TabsFiltersProps): JSX.Element {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);

  const handleKey = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
      if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
      event.preventDefault();
      const dir = event.key === 'ArrowRight' ? 1 : -1;
      const nextIndex = (index + dir + TABS.length) % TABS.length;
      const nextTab = TABS[nextIndex];
      if (!nextTab) return;
      onChange(nextTab.value);
      refs.current[nextIndex]?.focus();
    },
    [onChange],
  );

  return (
    <div
      role="tablist"
      aria-label="Фильтр изменений"
      data-testid="tabs-filters"
      className={cn('flex flex-wrap items-center gap-1 border-b border-border', className)}
    >
      {TABS.map((tab, index) => {
        const isActive = tab.value === value;
        const counter = counters?.[tab.value];
        return (
          <button
            key={tab.value}
            ref={(node) => {
              refs.current[index] = node;
            }}
            type="button"
            role="tab"
            aria-selected={isActive}
            tabIndex={isActive ? 0 : -1}
            data-testid={`tabs-filters-${tab.value}`}
            onClick={() => onChange(tab.value)}
            onKeyDown={(event) => handleKey(event, index)}
            className={cn(
              'inline-flex items-center gap-2 px-3 py-2 text-sm transition-colors',
              'border-b-2 -mb-px focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1',
              isActive
                ? 'border-brand-600 text-brand-600 font-medium'
                : 'border-transparent text-fg-muted hover:text-fg',
            )}
          >
            <span>{tab.label}</span>
            {counter !== undefined ? (
              <span className="rounded-sm bg-bg-muted px-1.5 py-0.5 text-xs font-medium text-fg-muted">
                {counter}
              </span>
            ) : null}
          </button>
        );
      })}
    </div>
  );
}
