import type { ReactNode } from 'react';

export interface FilterOption<V extends string = string> {
  value: V;
  label: string;
  icon?: ReactNode;
}

export type FilterKind = 'multi' | 'single';

export interface FilterDefinition<V extends string = string> {
  key: string;
  label: string;
  kind: FilterKind;
  options: readonly FilterOption<V>[];
  defaultValue?: V | readonly V[];
  pinned?: boolean;
}

export type FilterValue = string | readonly string[];
export type FilterGroupValue = Readonly<Record<string, FilterValue>>;

export interface UseFilterParamsOptions {
  definitions: readonly FilterDefinition[];
  replace?: boolean;
}

export interface UseFilterParamsResult {
  values: FilterGroupValue;
  setValue: (key: string, next: FilterValue) => void;
  toggleOption: (key: string, value: string) => void;
  clear: (key?: string) => void;
  activeCount: number;
}
