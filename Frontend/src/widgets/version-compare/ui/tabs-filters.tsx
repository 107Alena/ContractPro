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
      className={cn('flex flex-wrap items-center gap-2', className)}
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
              'inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-sm transition-colors',
              'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1',
              isActive
                ? 'border-brand-600 bg-brand-600 font-medium text-white'
                : 'border-border-subtle bg-bg text-fg-muted hover:text-fg',
            )}
          >
            <span>{tab.label}</span>
            {counter !== undefined ? (
              <span
                className={cn(
                  'rounded-full px-1.5 py-0.5 text-xs font-medium',
                  isActive ? 'bg-white/20 text-white' : 'bg-bg-muted text-fg-muted',
                )}
              >
                {counter}
              </span>
            ) : null}
          </button>
        );
      })}
    </div>
  );
}
