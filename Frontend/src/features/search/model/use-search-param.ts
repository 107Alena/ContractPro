import { useCallback } from 'react';
import { useSearchParams } from 'react-router-dom';

import type { UseSearchParamOptions } from './types';

export type { UseSearchParamOptions };

export function useSearchParam(
  opts: UseSearchParamOptions,
): [value: string, setValue: (next: string) => void] {
  const { key, defaultValue = '', replace = false } = opts;
  const [searchParams, setSearchParams] = useSearchParams();
  const current = searchParams.get(key) ?? defaultValue;

  const setValue = useCallback(
    (next: string) => {
      setSearchParams(
        (prev) => {
          const np = new URLSearchParams(prev);
          const trimmed = next === defaultValue ? '' : next;
          if (trimmed === '') {
            np.delete(key);
          } else {
            np.set(key, trimmed);
          }
          return np;
        },
        { replace },
      );
    },
    [setSearchParams, key, defaultValue, replace],
  );

  return [current, setValue];
}
