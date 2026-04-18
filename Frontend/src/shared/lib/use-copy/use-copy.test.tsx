// @vitest-environment jsdom
//
// useCopy — Clipboard API happy path + execCommand fallback + resetMs таймер.
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useCopy } from './use-copy';

type WritableClipboard = { writeText: (text: string) => Promise<void> };

const originalClipboard = Object.getOwnPropertyDescriptor(globalThis.navigator, 'clipboard');

function setClipboard(value: WritableClipboard | undefined): void {
  Object.defineProperty(globalThis.navigator, 'clipboard', {
    value,
    configurable: true,
    writable: true,
  });
}

function restoreClipboard(): void {
  if (originalClipboard) {
    Object.defineProperty(globalThis.navigator, 'clipboard', originalClipboard);
  } else {
    setClipboard(undefined);
  }
}

const originalExecCommand = Object.getOwnPropertyDescriptor(Document.prototype, 'execCommand');

function setExecCommand(fn: (cmd: string) => boolean): void {
  Object.defineProperty(document, 'execCommand', {
    value: fn,
    configurable: true,
    writable: true,
  });
}

function restoreExecCommand(): void {
  if (originalExecCommand) {
    Object.defineProperty(Document.prototype, 'execCommand', originalExecCommand);
  } else {
    // jsdom 24 не реализует execCommand — удаляем полифил, добавленный в тесте.
    delete (document as { execCommand?: unknown }).execCommand;
  }
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  restoreClipboard();
  restoreExecCommand();
  vi.restoreAllMocks();
});

describe('useCopy — clipboard path', () => {
  it('navigator.clipboard.writeText вызывается с переданным текстом', async () => {
    const writeText = vi.fn<[string], Promise<void>>().mockResolvedValue(undefined);
    setClipboard({ writeText });

    const { result } = renderHook(() => useCopy());
    let ok = false;
    await act(async () => {
      ok = await result.current.copy('hello');
    });
    expect(ok).toBe(true);
    expect(writeText).toHaveBeenCalledWith('hello');
    expect(result.current.copied).toBe(true);
  });

  it('copied автоматически сбрасывается через resetMs', async () => {
    setClipboard({ writeText: vi.fn().mockResolvedValue(undefined) });
    const { result } = renderHook(() => useCopy(1500));

    await act(async () => {
      await result.current.copy('x');
    });
    expect(result.current.copied).toBe(true);
    act(() => {
      vi.advanceTimersByTime(1500);
    });
    expect(result.current.copied).toBe(false);
  });

  it('повторный copy до истечения таймера продляет window', async () => {
    setClipboard({ writeText: vi.fn().mockResolvedValue(undefined) });
    const { result } = renderHook(() => useCopy(1000));

    await act(async () => {
      await result.current.copy('a');
    });
    act(() => {
      vi.advanceTimersByTime(500);
    });
    await act(async () => {
      await result.current.copy('b');
    });
    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(result.current.copied).toBe(true); // второй вызов продлил до 1000 мс от b
    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(result.current.copied).toBe(false);
  });
});

describe('useCopy — execCommand fallback', () => {
  it('при отсутствии navigator.clipboard используется document.execCommand', async () => {
    setClipboard(undefined);
    const execSpy = vi.fn(() => true);
    setExecCommand(execSpy);

    const { result } = renderHook(() => useCopy());
    let ok = false;
    await act(async () => {
      ok = await result.current.copy('fallback');
    });
    expect(ok).toBe(true);
    expect(execSpy).toHaveBeenCalledWith('copy');
    expect(result.current.copied).toBe(true);
  });

  it('при ошибке clipboard.writeText — падение на execCommand', async () => {
    setClipboard({ writeText: vi.fn().mockRejectedValue(new Error('denied')) });
    const execSpy = vi.fn(() => true);
    setExecCommand(execSpy);

    const { result } = renderHook(() => useCopy());
    let ok = false;
    await act(async () => {
      ok = await result.current.copy('retry');
    });
    expect(ok).toBe(true);
    expect(execSpy).toHaveBeenCalledWith('copy');
  });

  it('execCommand возвращает false → copied остаётся false', async () => {
    setClipboard(undefined);
    setExecCommand(() => false);
    const { result } = renderHook(() => useCopy());
    let ok = true;
    await act(async () => {
      ok = await result.current.copy('nope');
    });
    expect(ok).toBe(false);
    expect(result.current.copied).toBe(false);
  });
});
