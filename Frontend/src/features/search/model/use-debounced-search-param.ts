import { useCallback, useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

import type { UseDebouncedSearchParamOptions, UseDebouncedSearchParamResult } from './types';

const DEFAULT_DEBOUNCE_MS = 300;

export type { UseDebouncedSearchParamOptions, UseDebouncedSearchParamResult };

export function useDebouncedSearchParam(
  opts: UseDebouncedSearchParamOptions,
): UseDebouncedSearchParamResult {
  const {
    key,
    defaultValue = '',
    debounceMs = DEFAULT_DEBOUNCE_MS,
    minLength = 0,
    replace = true,
  } = opts;

  const [searchParams, setSearchParams] = useSearchParams();
  const urlValue = searchParams.get(key) ?? defaultValue;

  const [inputValue, setInputValueState] = useState(urlValue);
  const [committedValue, setCommittedValue] = useState(urlValue);

  const latestInputRef = useRef(inputValue);
  latestInputRef.current = inputValue;
  const keyRef = useRef(key);
  keyRef.current = key;
  const defaultValueRef = useRef(defaultValue);
  defaultValueRef.current = defaultValue;
  const minLengthRef = useRef(minLength);
  minLengthRef.current = minLength;
  const replaceRef = useRef(replace);
  replaceRef.current = replace;

  const lastUrlValueRef = useRef(urlValue);
  useEffect(() => {
    if (urlValue !== lastUrlValueRef.current && urlValue !== latestInputRef.current) {
      setInputValueState(urlValue);
      setCommittedValue(urlValue);
    }
    lastUrlValueRef.current = urlValue;
  }, [urlValue]);

  useEffect(() => {
    if (inputValue === committedValue) return;

    function commit(next: string): void {
      const effective = next.length < minLengthRef.current ? defaultValueRef.current : next;
      setCommittedValue(effective);
      setSearchParams(
        (prev) => {
          const np = new URLSearchParams(prev);
          if (effective === defaultValueRef.current || effective === '') {
            np.delete(keyRef.current);
          } else {
            np.set(keyRef.current, effective);
          }
          return np;
        },
        { replace: replaceRef.current },
      );
    }

    if (debounceMs <= 0) {
      commit(inputValue);
      return;
    }
    const id = setTimeout(() => {
      if (latestInputRef.current === inputValue) {
        commit(inputValue);
      }
    }, debounceMs);
    return () => clearTimeout(id);
  }, [inputValue, committedValue, debounceMs, setSearchParams]);

  const setInputValue = useCallback((next: string) => {
    setInputValueState(next);
  }, []);

  const clear = useCallback(() => {
    setInputValueState(defaultValueRef.current);
    setCommittedValue(defaultValueRef.current);
    setSearchParams(
      (prev) => {
        const np = new URLSearchParams(prev);
        np.delete(keyRef.current);
        return np;
      },
      { replace: replaceRef.current },
    );
  }, [setSearchParams]);

  return {
    inputValue,
    committedValue,
    isPending: inputValue !== committedValue,
    setInputValue,
    clear,
  };
}
