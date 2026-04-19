import { useCallback, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';

import { isDefault, parseFilterParams, serializeFilterParams } from './filter-params';
import type {
  FilterGroupValue,
  FilterValue,
  UseFilterParamsOptions,
  UseFilterParamsResult,
} from './types';

export function useFilterParams(opts: UseFilterParamsOptions): UseFilterParamsResult {
  const { definitions, replace = false } = opts;
  const [searchParams, setSearchParams] = useSearchParams();

  const values: FilterGroupValue = useMemo(
    () => parseFilterParams(searchParams, definitions),
    [searchParams, definitions],
  );

  const commit = useCallback(
    (patch: Partial<Record<string, FilterValue>>) => {
      setSearchParams(
        (prev) => {
          const merged: Record<string, FilterValue> = {
            ...parseFilterParams(prev, definitions),
          };
          for (const [k, v] of Object.entries(patch)) {
            if (v !== undefined) merged[k] = v;
          }
          return serializeFilterParams(prev, definitions, merged);
        },
        { replace },
      );
    },
    [setSearchParams, definitions, replace],
  );

  const setValue = useCallback(
    (key: string, next: FilterValue) => {
      commit({ [key]: next });
    },
    [commit],
  );

  const toggleOption = useCallback(
    (key: string, value: string) => {
      const def = definitions.find((d) => d.key === key);
      if (!def) return;
      if (def.kind === 'multi') {
        const current = values[key];
        const list = Array.isArray(current) ? [...current] : [];
        const idx = list.indexOf(value);
        if (idx >= 0) list.splice(idx, 1);
        else list.push(value);
        commit({ [key]: list });
      } else {
        const current = typeof values[key] === 'string' ? (values[key] as string) : '';
        commit({ [key]: current === value ? '' : value });
      }
    },
    [definitions, values, commit],
  );

  const clear = useCallback(
    (key?: string) => {
      if (key === undefined) {
        const reset: Record<string, FilterValue> = {};
        for (const def of definitions) {
          reset[def.key] = def.kind === 'multi' ? [] : '';
        }
        commit(reset);
      } else {
        const def = definitions.find((d) => d.key === key);
        if (!def) return;
        commit({ [key]: def.kind === 'multi' ? [] : '' });
      }
    },
    [definitions, commit],
  );

  const activeCount = useMemo(
    () => definitions.filter((def) => !isDefault(def, values[def.key] ?? '')).length,
    [definitions, values],
  );

  return { values, setValue, toggleOption, clear, activeCount };
}
