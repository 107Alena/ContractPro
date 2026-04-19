// @vitest-environment jsdom
//
// useDebounce / useDebouncedCallback — generic debounce utility.
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useDebounce, useDebouncedCallback } from './use-debounce';

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('useDebounce', () => {
  it('возвращает initial value немедленно на первом рендере', () => {
    const { result } = renderHook(() => useDebounce('initial', 300));
    expect(result.current).toBe('initial');
  });

  it('не обновляется до истечения delayMs', () => {
    const { result, rerender } = renderHook(({ v }: { v: string }) => useDebounce(v, 300), {
      initialProps: { v: 'a' },
    });
    rerender({ v: 'b' });
    expect(result.current).toBe('a');
    act(() => {
      vi.advanceTimersByTime(299);
    });
    expect(result.current).toBe('a');
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(result.current).toBe('b');
  });

  it('серия быстрых изменений — коалесцируется в последнее значение', () => {
    const { result, rerender } = renderHook(({ v }: { v: string }) => useDebounce(v, 200), {
      initialProps: { v: 'a' },
    });
    rerender({ v: 'b' });
    act(() => {
      vi.advanceTimersByTime(50);
    });
    rerender({ v: 'c' });
    act(() => {
      vi.advanceTimersByTime(50);
    });
    rerender({ v: 'd' });
    act(() => {
      vi.advanceTimersByTime(199);
    });
    expect(result.current).toBe('a');
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(result.current).toBe('d');
  });

  it('delayMs <= 0 — значение синхронизируется немедленно при rerender', () => {
    const { result, rerender } = renderHook(({ v }: { v: number }) => useDebounce(v, 0), {
      initialProps: { v: 1 },
    });
    rerender({ v: 2 });
    expect(result.current).toBe(2);
  });

  it('generic работает с объектом', () => {
    const a = { id: 1 };
    const b = { id: 2 };
    const { result, rerender } = renderHook(({ v }: { v: { id: number } }) => useDebounce(v, 100), {
      initialProps: { v: a },
    });
    rerender({ v: b });
    act(() => {
      vi.advanceTimersByTime(100);
    });
    expect(result.current).toBe(b);
  });

  it('unmount очищает таймер — нет setState после unmount', () => {
    const { rerender, unmount } = renderHook(({ v }: { v: string }) => useDebounce(v, 300), {
      initialProps: { v: 'a' },
    });
    rerender({ v: 'b' });
    unmount();
    expect(() => {
      act(() => {
        vi.advanceTimersByTime(1000);
      });
    }).not.toThrow();
  });
});

describe('useDebouncedCallback', () => {
  it('вызывается только через delayMs после последнего вызова', () => {
    const fn = vi.fn();
    const { result } = renderHook(() => useDebouncedCallback(fn, 200));
    act(() => {
      result.current('a');
    });
    expect(fn).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(200);
    });
    expect(fn).toHaveBeenCalledTimes(1);
    expect(fn).toHaveBeenCalledWith('a');
  });

  it('серия вызовов — коалесцируется в один с последним аргументом', () => {
    const fn = vi.fn();
    const { result } = renderHook(() => useDebouncedCallback(fn, 200));
    act(() => {
      result.current('a');
      result.current('b');
      result.current('c');
    });
    act(() => {
      vi.advanceTimersByTime(200);
    });
    expect(fn).toHaveBeenCalledTimes(1);
    expect(fn).toHaveBeenCalledWith('c');
  });

  it('cancel отменяет pending-вызов', () => {
    const fn = vi.fn();
    const { result } = renderHook(() => useDebouncedCallback(fn, 200));
    act(() => {
      result.current('x');
      result.current.cancel();
      vi.advanceTimersByTime(300);
    });
    expect(fn).not.toHaveBeenCalled();
  });

  it('flush — синхронно выполняет pending с последним аргументом', () => {
    const fn = vi.fn();
    const { result } = renderHook(() => useDebouncedCallback(fn, 200));
    act(() => {
      result.current('x');
      result.current('y');
      result.current.flush();
    });
    expect(fn).toHaveBeenCalledTimes(1);
    expect(fn).toHaveBeenCalledWith('y');
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it('смена fn-ссылки не ресетит pending-таймер', () => {
    const first = vi.fn();
    const second = vi.fn();
    const { result, rerender } = renderHook(
      ({ fn }: { fn: (v: string) => void }) => useDebouncedCallback(fn, 200),
      { initialProps: { fn: first } },
    );
    act(() => {
      result.current('a');
    });
    rerender({ fn: second });
    act(() => {
      vi.advanceTimersByTime(200);
    });
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledWith('a');
  });

  it('delayMs <= 0 — вызывается синхронно', () => {
    const fn = vi.fn();
    const { result } = renderHook(() => useDebouncedCallback(fn, 0));
    act(() => {
      result.current('immediate');
    });
    expect(fn).toHaveBeenCalledWith('immediate');
  });

  it('unmount очищает pending', () => {
    const fn = vi.fn();
    const { result, unmount } = renderHook(() => useDebouncedCallback(fn, 200));
    act(() => {
      result.current('x');
    });
    unmount();
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(fn).not.toHaveBeenCalled();
  });
});
