// useContractsSort — серверная сортировка списка договоров через URL-state
// (FE-TASK-058, Figma 200:7). Отдельный хук (не useFilterParams): сортировка —
// не фильтр (не входит в activeCount, не рендерится как removable-чип, иначе
// сбрасывается). Зеркалит паттерн usePageParams: парсинг с валидацией против
// enum-объединений из ListParams, delete-on-default (чистый URL), смена
// сортировки сбрасывает на 1-ю страницу той же транзакцией setSearchParams.
//
// Источник истины — URL (single source): значение не дублируется в useState,
// чтобы back/forward-навигация не рассинхронизировала контрол.
import { useCallback, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';

import { PAGE_PARAM_KEY } from '@/features/pagination';
import type { ListParams } from '@/shared/api';

export type SortField = NonNullable<ListParams['sort']>; // 'date' | 'title' | 'risk'
export type SortOrder = NonNullable<ListParams['order']>; // 'asc' | 'desc'

const SORT_PARAM_KEY = 'sort';
const ORDER_PARAM_KEY = 'order';

export const DEFAULT_SORT: SortField = 'date';
export const DEFAULT_ORDER: SortOrder = 'desc';

const SORT_FIELDS: readonly SortField[] = ['date', 'title', 'risk'];
const SORT_ORDERS: readonly SortOrder[] = ['asc', 'desc'];

export interface UseContractsSortResult {
  sort: SortField;
  order: SortOrder;
  /** true, если выбран дефолт (date desc) — тогда параметры не уходят в URL/API. */
  isDefault: boolean;
  setSort: (sort: SortField, order: SortOrder) => void;
}

export function useContractsSort(): UseContractsSortResult {
  const [searchParams, setSearchParams] = useSearchParams();

  const sort = useMemo<SortField>(() => {
    const raw = searchParams.get(SORT_PARAM_KEY);
    return raw != null && (SORT_FIELDS as readonly string[]).includes(raw)
      ? (raw as SortField)
      : DEFAULT_SORT;
  }, [searchParams]);

  const order = useMemo<SortOrder>(() => {
    const raw = searchParams.get(ORDER_PARAM_KEY);
    return raw != null && (SORT_ORDERS as readonly string[]).includes(raw)
      ? (raw as SortOrder)
      : DEFAULT_ORDER;
  }, [searchParams]);

  const setSort = useCallback(
    (nextSort: SortField, nextOrder: SortOrder) => {
      setSearchParams((prev) => {
        const np = new URLSearchParams(prev);
        if (nextSort === DEFAULT_SORT) np.delete(SORT_PARAM_KEY);
        else np.set(SORT_PARAM_KEY, nextSort);
        if (nextOrder === DEFAULT_ORDER) np.delete(ORDER_PARAM_KEY);
        else np.set(ORDER_PARAM_KEY, nextOrder);
        // Смена сортировки → на первую страницу (иначе показалась бы page-N уже
        // другого порядка). Та же транзакция, что и sort/order.
        np.delete(PAGE_PARAM_KEY);
        return np;
      });
    },
    [setSearchParams],
  );

  const isDefault = sort === DEFAULT_SORT && order === DEFAULT_ORDER;

  return { sort, order, isDefault, setSort };
}
