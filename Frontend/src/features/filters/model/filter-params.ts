import { fromCsv, toCsv } from '../lib/csv';
import type { FilterDefinition, FilterGroupValue, FilterValue } from './types';

function defaultFor(def: FilterDefinition): FilterValue {
  if (def.kind === 'multi') {
    return def.defaultValue != null && Array.isArray(def.defaultValue)
      ? (def.defaultValue as readonly string[])
      : [];
  }
  return typeof def.defaultValue === 'string' ? def.defaultValue : '';
}

export function parseFilterParams(
  params: URLSearchParams,
  definitions: readonly FilterDefinition[],
): FilterGroupValue {
  const result: Record<string, FilterValue> = {};
  for (const def of definitions) {
    const raw = params.get(def.key);
    if (def.kind === 'multi') {
      result[def.key] = fromCsv(raw);
    } else {
      result[def.key] = raw ?? (typeof def.defaultValue === 'string' ? def.defaultValue : '');
    }
  }
  return result;
}

export function serializeFilterParams(
  current: URLSearchParams,
  definitions: readonly FilterDefinition[],
  values: FilterGroupValue,
): URLSearchParams {
  const next = new URLSearchParams(current);
  for (const def of definitions) {
    const value = values[def.key];
    if (def.kind === 'multi') {
      const list = Array.isArray(value) ? (value as readonly string[]) : [];
      if (list.length === 0) {
        next.delete(def.key);
      } else {
        next.set(def.key, toCsv(list));
      }
    } else {
      const str = typeof value === 'string' ? value : '';
      const def0 = typeof def.defaultValue === 'string' ? def.defaultValue : '';
      if (str === '' || str === def0) {
        next.delete(def.key);
      } else {
        next.set(def.key, str);
      }
    }
  }
  return next;
}

export function isDefault(def: FilterDefinition, value: FilterValue): boolean {
  const def0 = defaultFor(def);
  if (def.kind === 'multi') {
    const a = Array.isArray(value) ? value : [];
    const b = Array.isArray(def0) ? def0 : [];
    return a.length === b.length && a.every((v, i) => v === b[i]);
  }
  const s = typeof value === 'string' ? value : '';
  const s0 = typeof def0 === 'string' ? def0 : '';
  return s === s0;
}
