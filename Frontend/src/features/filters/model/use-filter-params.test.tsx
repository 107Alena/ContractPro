// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { describe, expect, it } from 'vitest';

import type { FilterDefinition } from './types';
import { useFilterParams } from './use-filter-params';

const DEFS: readonly FilterDefinition[] = [
  {
    key: 'status',
    label: 'Статус',
    kind: 'single',
    options: [
      { value: 'ACTIVE', label: 'Активные' },
      { value: 'ARCHIVED', label: 'В архиве' },
    ],
  },
  {
    key: 'types',
    label: 'Типы',
    kind: 'multi',
    options: [
      { value: 'SUPPLY', label: 'Поставка' },
      { value: 'SERVICE', label: 'Услуги' },
    ],
  },
];

function wrapper(initial = '/') {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <MemoryRouter initialEntries={[initial]}>{children}</MemoryRouter>;
  };
}

describe('useFilterParams', () => {
  it('читает дефолтные значения из пустого URL', () => {
    const { result } = renderHook(() => useFilterParams({ definitions: DEFS }), {
      wrapper: wrapper('/'),
    });
    expect(result.current.values.status).toBe('');
    expect(result.current.values.types).toEqual([]);
    expect(result.current.activeCount).toBe(0);
  });

  it('читает URL-параметры (single + CSV multi)', () => {
    const { result } = renderHook(() => useFilterParams({ definitions: DEFS }), {
      wrapper: wrapper('/?status=ACTIVE&types=SUPPLY,SERVICE'),
    });
    expect(result.current.values.status).toBe('ACTIVE');
    expect(result.current.values.types).toEqual(['SUPPLY', 'SERVICE']);
    expect(result.current.activeCount).toBe(2);
  });

  it('setValue single — меняет URL', () => {
    function useHook() {
      const h = useFilterParams({ definitions: DEFS });
      const location = useLocation();
      return { ...h, search: location.search };
    }
    const { result } = renderHook(() => useHook(), { wrapper: wrapper('/') });
    act(() => {
      result.current.setValue('status', 'ACTIVE');
    });
    expect(result.current.search).toContain('status=ACTIVE');
  });

  it('toggleOption multi: добавляет/удаляет значение', () => {
    function useHook() {
      const h = useFilterParams({ definitions: DEFS });
      const location = useLocation();
      return { ...h, search: location.search };
    }
    const { result } = renderHook(() => useHook(), { wrapper: wrapper('/') });
    act(() => {
      result.current.toggleOption('types', 'SUPPLY');
    });
    expect(result.current.search).toContain('types=SUPPLY');
    act(() => {
      result.current.toggleOption('types', 'SERVICE');
    });
    expect(result.current.search).toMatch(/types=SUPPLY%2CSERVICE|types=SUPPLY,SERVICE/);
    act(() => {
      result.current.toggleOption('types', 'SUPPLY');
    });
    expect(result.current.search).toMatch(/types=SERVICE/);
  });

  it('toggleOption single: повторный клик снимает выбор', () => {
    function useHook() {
      const h = useFilterParams({ definitions: DEFS });
      const location = useLocation();
      return { ...h, search: location.search };
    }
    const { result } = renderHook(() => useHook(), { wrapper: wrapper('/?status=ACTIVE') });
    act(() => {
      result.current.toggleOption('status', 'ACTIVE');
    });
    expect(result.current.search).not.toContain('status=');
  });

  it('clear() очищает все фильтры', () => {
    function useHook() {
      const h = useFilterParams({ definitions: DEFS });
      const location = useLocation();
      return { ...h, search: location.search };
    }
    const { result } = renderHook(() => useHook(), {
      wrapper: wrapper('/?status=ACTIVE&types=SUPPLY'),
    });
    act(() => {
      result.current.clear();
    });
    expect(result.current.search).toBe('');
  });

  it('clear(key) очищает только указанный', () => {
    function useHook() {
      const h = useFilterParams({ definitions: DEFS });
      const location = useLocation();
      return { ...h, search: location.search };
    }
    const { result } = renderHook(() => useHook(), {
      wrapper: wrapper('/?status=ACTIVE&types=SUPPLY'),
    });
    act(() => {
      result.current.clear('status');
    });
    expect(result.current.search).not.toContain('status=');
    expect(result.current.search).toContain('types=SUPPLY');
  });
});
