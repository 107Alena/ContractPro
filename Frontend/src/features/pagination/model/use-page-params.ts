import { useCallback, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';

import { DEFAULT_PAGE_SIZE, PAGE_PARAM_KEY, PAGE_SIZE_OPTIONS, SIZE_PARAM_KEY } from './constants';
import type { PageParams, UsePageParamsOptions, UsePageParamsResult } from './types';

function toPositiveInt(raw: string | null): number | null {
  if (raw == null) return null;
  const n = Number(raw);
  if (!Number.isFinite(n)) return null;
  const i = Math.floor(n);
  return i >= 1 ? i : null;
}

export function usePageParams(opts: UsePageParamsOptions = {}): UsePageParamsResult {
  const {
    defaultSize = DEFAULT_PAGE_SIZE,
    allowedSizes = PAGE_SIZE_OPTIONS,
    pageKey = PAGE_PARAM_KEY,
    sizeKey = SIZE_PARAM_KEY,
    replace = false,
  } = opts;

  const [searchParams, setSearchParams] = useSearchParams();

  const page = useMemo(() => {
    const parsed = toPositiveInt(searchParams.get(pageKey));
    return parsed ?? 1;
  }, [searchParams, pageKey]);

  const size = useMemo(() => {
    const parsed = toPositiveInt(searchParams.get(sizeKey));
    if (parsed == null) return defaultSize;
    if (allowedSizes.includes(parsed)) return parsed;
    return defaultSize;
  }, [searchParams, sizeKey, defaultSize, allowedSizes]);

  const commit = useCallback(
    (next: PageParams) => {
      setSearchParams(
        (prev) => {
          const np = new URLSearchParams(prev);
          if (next.page === 1) np.delete(pageKey);
          else np.set(pageKey, String(next.page));
          if (next.size === defaultSize) np.delete(sizeKey);
          else np.set(sizeKey, String(next.size));
          return np;
        },
        { replace },
      );
    },
    [setSearchParams, pageKey, sizeKey, defaultSize, replace],
  );

  const setPage = useCallback(
    (next: number) => {
      const clamped = Math.max(1, Math.floor(next));
      commit({ page: clamped, size });
    },
    [commit, size],
  );

  const setSize = useCallback(
    (next: number) => {
      const clamped = allowedSizes.includes(next) ? next : defaultSize;
      commit({ page: 1, size: clamped });
    },
    [commit, allowedSizes, defaultSize],
  );

  const setPageAndSize = useCallback(
    (next: PageParams) => {
      const p = Math.max(1, Math.floor(next.page));
      const s = allowedSizes.includes(next.size) ? next.size : defaultSize;
      commit({ page: p, size: s });
    },
    [commit, allowedSizes, defaultSize],
  );

  const reset = useCallback(() => {
    setSearchParams(
      (prev) => {
        const np = new URLSearchParams(prev);
        np.delete(pageKey);
        np.delete(sizeKey);
        return np;
      },
      { replace },
    );
  }, [setSearchParams, pageKey, sizeKey, replace]);

  return { page, size, setPage, setSize, setPageAndSize, reset };
}
