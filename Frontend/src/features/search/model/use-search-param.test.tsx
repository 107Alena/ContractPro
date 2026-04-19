// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { describe, expect, it } from 'vitest';

import { useSearchParam } from './use-search-param';

function wrapper(initial = '/') {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <MemoryRouter initialEntries={[initial]}>{children}</MemoryRouter>;
  };
}

describe('useSearchParam', () => {
  it('возвращает defaultValue если ключа нет в URL', () => {
    const { result } = renderHook(() => useSearchParam({ key: 'q', defaultValue: '' }), {
      wrapper: wrapper('/'),
    });
    expect(result.current[0]).toBe('');
  });

  it('возвращает текущее значение из URL', () => {
    const { result } = renderHook(() => useSearchParam({ key: 'q' }), {
      wrapper: wrapper('/?q=hello'),
    });
    expect(result.current[0]).toBe('hello');
  });

  it('setValue обновляет URL', () => {
    function useHook() {
      const [value, setValue] = useSearchParam({ key: 'q' });
      const location = useLocation();
      return { value, setValue, search: location.search };
    }
    const { result } = renderHook(() => useHook(), { wrapper: wrapper('/') });
    act(() => {
      result.current.setValue('world');
    });
    expect(result.current.search).toContain('q=world');
  });

  it('setValue("") удаляет ключ из URL', () => {
    function useHook() {
      const [value, setValue] = useSearchParam({ key: 'q' });
      const location = useLocation();
      return { value, setValue, search: location.search };
    }
    const { result } = renderHook(() => useHook(), { wrapper: wrapper('/?q=a') });
    act(() => {
      result.current.setValue('');
    });
    expect(result.current.search).not.toContain('q=');
  });

  it('при двух разных ключах функциональный апдейтер не теряет соседний ключ', () => {
    function useHook() {
      const [q, setQ] = useSearchParam({ key: 'q' });
      const [page, setPage] = useSearchParam({ key: 'page' });
      const location = useLocation();
      return { q, setQ, page, setPage, search: location.search };
    }
    const { result } = renderHook(() => useHook(), { wrapper: wrapper('/?page=2') });
    act(() => {
      result.current.setQ('x');
    });
    expect(result.current.search).toContain('q=x');
    expect(result.current.search).toContain('page=2');
  });
});
