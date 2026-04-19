import { useMemo, useState } from 'react';

import { Button } from '@/shared/ui/button';
import { Chip } from '@/shared/ui/chip';

import { isDefault } from '../model/filter-params';
import type { FilterDefinition, FilterGroupValue, FilterOption, FilterValue } from '../model/types';
import { MoreFiltersModal } from './MoreFiltersModal';

export interface FilterChipsProps {
  definitions: readonly FilterDefinition[];
  values: FilterGroupValue;
  onToggleOption: (key: string, value: string) => void;
  onClear: (key?: string) => void;
  /** true — показывать «Ещё фильтры» кнопку, когда есть non-pinned filters. */
  showMoreButton?: boolean;
  /** Класс для контейнера. */
  className?: string;
  /** aria-label на корневом списке. */
  ariaLabel?: string;
}

function hasActiveValue(def: FilterDefinition, value: FilterValue): boolean {
  return !isDefault(def, value);
}

function findOption(def: FilterDefinition, value: string): FilterOption | undefined {
  return def.options.find((o) => o.value === value);
}

export function FilterChips({
  definitions,
  values,
  onToggleOption,
  onClear,
  showMoreButton = true,
  className,
  ariaLabel = 'Фильтры',
}: FilterChipsProps) {
  const [moreOpen, setMoreOpen] = useState(false);

  const pinnedDefs = useMemo(() => definitions.filter((d) => d.pinned !== false), [definitions]);
  const nonPinnedDefs = useMemo(() => definitions.filter((d) => d.pinned === false), [definitions]);

  const hasAnyActive = useMemo(
    () => definitions.some((def) => hasActiveValue(def, values[def.key] ?? '')),
    [definitions, values],
  );

  const activeTokens = useMemo(() => {
    const tokens: Array<{
      key: string;
      value: string;
      label: string;
      filterKey: string;
    }> = [];
    for (const def of definitions) {
      const v = values[def.key];
      if (def.kind === 'multi') {
        const list = Array.isArray(v) ? v : [];
        for (const val of list) {
          const opt = findOption(def, val);
          if (opt) {
            tokens.push({
              key: `${def.key}:${val}`,
              value: val,
              label: opt.label,
              filterKey: def.key,
            });
          }
        }
      } else {
        const val = typeof v === 'string' ? v : '';
        if (val !== '' && !isDefault(def, val)) {
          const opt = findOption(def, val);
          if (opt) {
            tokens.push({
              key: `${def.key}:${val}`,
              value: val,
              label: opt.label,
              filterKey: def.key,
            });
          }
        }
      }
    }
    return tokens;
  }, [definitions, values]);

  return (
    <div className={className}>
      <div role="group" aria-label={ariaLabel} className="flex flex-wrap items-center gap-2">
        {pinnedDefs.map((def) => (
          <div key={def.key} className="flex flex-wrap gap-1">
            {def.options.map((opt) => {
              const v = values[def.key];
              const selected =
                def.kind === 'multi' ? Array.isArray(v) && v.includes(opt.value) : v === opt.value;
              return (
                <Chip
                  key={opt.value}
                  selected={selected}
                  interactive
                  onClick={() => onToggleOption(def.key, opt.value)}
                  data-testid={`filter-chip-${def.key}-${opt.value}`}
                >
                  {opt.icon}
                  {opt.label}
                </Chip>
              );
            })}
          </div>
        ))}
        {showMoreButton && nonPinnedDefs.length > 0 ? (
          <Button
            type="button"
            variant="secondary"
            size="sm"
            onClick={() => setMoreOpen(true)}
            data-testid="filter-chips-more"
          >
            Ещё фильтры
          </Button>
        ) : null}
        {hasAnyActive ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => onClear()}
            data-testid="filter-chips-clear"
          >
            Сбросить
          </Button>
        ) : null}
      </div>
      {activeTokens.length > 0 ? (
        <div className="mt-2 flex flex-wrap gap-2" aria-label="Активные фильтры" role="list">
          {activeTokens.map((t) => (
            <div key={t.key} role="listitem">
              <Chip
                onRemove={() => onToggleOption(t.filterKey, t.value)}
                removeLabel={`Убрать фильтр ${t.label}`}
              >
                {t.label}
              </Chip>
            </div>
          ))}
        </div>
      ) : null}
      {showMoreButton && nonPinnedDefs.length > 0 ? (
        <MoreFiltersModal
          open={moreOpen}
          onOpenChange={setMoreOpen}
          definitions={nonPinnedDefs}
          values={values}
          onToggleOption={onToggleOption}
          onClear={onClear}
        />
      ) : null}
    </div>
  );
}
