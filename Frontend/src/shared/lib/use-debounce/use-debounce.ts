import { useCallback, useEffect, useRef, useState } from 'react';

export function useDebounce<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState<T>(value);

  useEffect(() => {
    if (delayMs <= 0) {
      setDebounced(value);
      return;
    }
    const id = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(id);
  }, [value, delayMs]);

  return debounced;
}

export type UseDebouncedCallback<A extends unknown[]> = {
  (...args: A): void;
  cancel: () => void;
  flush: () => void;
};

export function useDebouncedCallback<A extends unknown[]>(
  fn: (...args: A) => void,
  delayMs: number,
): UseDebouncedCallback<A> {
  const fnRef = useRef(fn);
  const delayRef = useRef(delayMs);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastArgsRef = useRef<A | null>(null);

  useEffect(() => {
    fnRef.current = fn;
  }, [fn]);
  useEffect(() => {
    delayRef.current = delayMs;
  }, [delayMs]);

  useEffect(() => {
    return () => {
      if (timerRef.current !== null) clearTimeout(timerRef.current);
    };
  }, []);

  const cancel = useCallback(() => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    lastArgsRef.current = null;
  }, []);

  const flush = useCallback(() => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    if (lastArgsRef.current !== null) {
      const args = lastArgsRef.current;
      lastArgsRef.current = null;
      fnRef.current(...args);
    }
  }, []);

  const debounced = useCallback((...args: A) => {
    lastArgsRef.current = args;
    if (delayRef.current <= 0) {
      const pending = lastArgsRef.current;
      lastArgsRef.current = null;
      fnRef.current(...pending);
      return;
    }
    if (timerRef.current !== null) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => {
      timerRef.current = null;
      if (lastArgsRef.current === null) return;
      const pending = lastArgsRef.current;
      lastArgsRef.current = null;
      fnRef.current(...pending);
    }, delayRef.current);
  }, []) as UseDebouncedCallback<A>;

  debounced.cancel = cancel;
  debounced.flush = flush;
  return debounced;
}
