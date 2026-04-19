// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { describe, expect, it } from 'vitest';

import { usePageParams } from './use-page-params';

function wrapper(initial = '/') {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <MemoryRouter initialEntries={[initial]}>{children}</MemoryRouter>;
  };
}

describe('usePageParams', () => {
  it('пустой URL → page=1, size=20 (defaults)', () => {
    const { result } = renderHook(() => usePageParams(), { wrapper: wrapper('/') });
    expect(result.current.page).toBe(1);
    expect(result.current.size).toBe(20);
  });

  it('читает page и size из URL', () => {
    const { result } = renderHook(() => usePageParams(), { wrapper: wrapper('/?page=3&size=50') });
    expect(result.current.page).toBe(3);
    expect(result.current.size).toBe(50);
  });

  it('неверный page → 1 (без редиректа URL)', () => {
    function useHook() {
      const h = usePageParams();
      const loc = useLocation();
      return { ...h, search: loc.search };
    }
    const { result } = renderHook(() => useHook(), { wrapper: wrapper('/?page=abc') });
    expect(result.current.page).toBe(1);
    expect(result.current.search).toContain('page=abc');
  });

  it('size вне allowedSizes → defaultSize', () => {
    const { result } = renderHook(() => usePageParams({ allowedSizes: [10, 20] }), {
      wrapper: wrapper('/?size=999'),
    });
    expect(result.current.size).toBe(20);
  });

  it('setPage меняет URL, page=1 удаляется', () => {
    function useHook() {
      const h = usePageParams();
      const loc = useLocation();
      return { ...h, search: loc.search };
    }
    const { result } = renderHook(() => useHook(), { wrapper: wrapper('/') });
    act(() => {
      result.current.setPage(3);
    });
    expect(result.current.search).toContain('page=3');
    act(() => {
      result.current.setPage(1);
    });
    expect(result.current.search).not.toContain('page=');
  });

  it('setSize сбрасывает page в 1 и обновляет URL; default size удаляется', () => {
    function useHook() {
      const h = usePageParams();
      const loc = useLocation();
      return { ...h, search: loc.search };
    }
    const { result } = renderHook(() => useHook(), { wrapper: wrapper('/?page=3&size=50') });
    act(() => {
      result.current.setSize(100);
    });
    expect(result.current.search).not.toContain('page=');
    expect(result.current.search).toContain('size=100');
    act(() => {
      result.current.setSize(20);
    });
    expect(result.current.search).not.toContain('size=');
  });

  it('setPage клэмпит в [1, ∞)', () => {
    function useHook() {
      const h = usePageParams();
      return h;
    }
    const { result } = renderHook(() => useHook(), { wrapper: wrapper('/') });
    act(() => {
      result.current.setPage(-5);
    });
    expect(result.current.page).toBe(1);
  });

  it('reset удаляет page и size', () => {
    function useHook() {
      const h = usePageParams();
      const loc = useLocation();
      return { ...h, search: loc.search };
    }
    const { result } = renderHook(() => useHook(), { wrapper: wrapper('/?page=3&size=50&q=x') });
    act(() => {
      result.current.reset();
    });
    expect(result.current.search).not.toContain('page=');
    expect(result.current.search).not.toContain('size=');
    expect(result.current.search).toContain('q=x');
  });
});
