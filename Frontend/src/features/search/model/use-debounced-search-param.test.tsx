// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useDebouncedSearchParam } from './use-debounced-search-param';

function wrapper(initial = '/') {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <MemoryRouter initialEntries={[initial]}>{children}</MemoryRouter>;
  };
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('useDebouncedSearchParam', () => {
  it('initial — inputValue и committedValue равны URL', () => {
    const { result } = renderHook(
      () => useDebouncedSearchParam({ key: 'search', debounceMs: 300 }),
      { wrapper: wrapper('/?search=abc') },
    );
    expect(result.current.inputValue).toBe('abc');
    expect(result.current.committedValue).toBe('abc');
    expect(result.current.isPending).toBe(false);
  });

  it('setInputValue меняет inputValue мгновенно, URL — после debounceMs', () => {
    function useHook() {
      const h = useDebouncedSearchParam({ key: 'search', debounceMs: 300 });
      const location = useLocation();
      return { ...h, search: location.search };
    }
    const { result } = renderHook(() => useHook(), { wrapper: wrapper('/') });
    act(() => {
      result.current.setInputValue('hello');
    });
    expect(result.current.inputValue).toBe('hello');
    expect(result.current.committedValue).toBe('');
    expect(result.current.isPending).toBe(true);
    expect(result.current.search).toBe('');
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(result.current.committedValue).toBe('hello');
    expect(result.current.isPending).toBe(false);
    expect(result.current.search).toContain('search=hello');
  });

  it('серия изменений в пределах окна — коалесцируется в последнее значение', () => {
    const { result } = renderHook(
      () => useDebouncedSearchParam({ key: 'search', debounceMs: 300 }),
      { wrapper: wrapper('/') },
    );
    act(() => {
      result.current.setInputValue('a');
    });
    act(() => {
      vi.advanceTimersByTime(100);
    });
    act(() => {
      result.current.setInputValue('ab');
    });
    act(() => {
      vi.advanceTimersByTime(100);
    });
    act(() => {
      result.current.setInputValue('abc');
    });
    act(() => {
      vi.advanceTimersByTime(299);
    });
    expect(result.current.committedValue).toBe('');
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(result.current.committedValue).toBe('abc');
  });

  it('minLength — пустое значение, если inputValue короче порога', () => {
    function useHook() {
      const h = useDebouncedSearchParam({
        key: 'search',
        debounceMs: 200,
        minLength: 3,
      });
      const location = useLocation();
      return { ...h, search: location.search };
    }
    const { result } = renderHook(() => useHook(), { wrapper: wrapper('/') });
    act(() => {
      result.current.setInputValue('ab');
    });
    act(() => {
      vi.advanceTimersByTime(200);
    });
    expect(result.current.committedValue).toBe('');
    expect(result.current.search).toBe('');
    act(() => {
      result.current.setInputValue('abc');
    });
    act(() => {
      vi.advanceTimersByTime(200);
    });
    expect(result.current.committedValue).toBe('abc');
    expect(result.current.search).toContain('search=abc');
  });

  it('clear синхронно сбрасывает всё', () => {
    function useHook() {
      const h = useDebouncedSearchParam({ key: 'search', debounceMs: 200 });
      const location = useLocation();
      return { ...h, search: location.search };
    }
    const { result } = renderHook(() => useHook(), {
      wrapper: wrapper('/?search=foo'),
    });
    act(() => {
      result.current.clear();
    });
    expect(result.current.inputValue).toBe('');
    expect(result.current.committedValue).toBe('');
    expect(result.current.isPending).toBe(false);
    expect(result.current.search).toBe('');
  });

  it('debounceMs <= 0 — коммит синхронный', () => {
    function useHook() {
      const h = useDebouncedSearchParam({ key: 'search', debounceMs: 0 });
      const location = useLocation();
      return { ...h, search: location.search };
    }
    const { result } = renderHook(() => useHook(), { wrapper: wrapper('/') });
    act(() => {
      result.current.setInputValue('fast');
    });
    expect(result.current.committedValue).toBe('fast');
    expect(result.current.search).toContain('search=fast');
  });
});
