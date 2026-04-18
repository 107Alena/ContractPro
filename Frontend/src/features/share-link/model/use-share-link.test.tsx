// @vitest-environment jsdom
//
// Хук-тест useShareLink: 302 → copy(location), copied флаг, onSuccess/onError,
// REQUEST_ABORTED фильтруется, ошибка clipboard не ломает mutation (copied=false).
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { AxiosInstance } from 'axios';
import { type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api';

import { __setHttpForTests } from '../api/http';
import { useShareLink } from './use-share-link';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const VERSION_ID = 'ba5e0000-aaaa-bbbb-cccc-111111111111';
const PRESIGNED = 'https://presigned.example/contractpro/report.pdf?X-Expires=300';

const INPUT = {
  contractId: CONTRACT_ID,
  versionId: VERSION_ID,
  format: 'pdf' as const,
};

function makeWrapper(): (props: { children: ReactNode }) => JSX.Element {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  function Wrapper({ children }: { children: ReactNode }): JSX.Element {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  }
  Wrapper.displayName = 'TestWrapper';
  return Wrapper;
}

function orch(code: string, status?: number): OrchestratorError {
  return new OrchestratorError(
    status !== undefined
      ? { error_code: code, message: code, status }
      : { error_code: code, message: code },
  );
}

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

let getSpy: ReturnType<typeof vi.fn>;
let writeSpy: ReturnType<typeof vi.fn>;

beforeEach(() => {
  // Fake timers включаются точечно в тестах, которым нужно
  // `vi.advanceTimersByTime` — иначе Testing Library `waitFor` зависает
  // (он опрашивает через setTimeout, который fake timers замораживают).
  getSpy = vi.fn();
  writeSpy = vi.fn().mockResolvedValue(undefined);
  setClipboard({ writeText: writeSpy as unknown as (t: string) => Promise<void> });
  __setHttpForTests({ get: getSpy } as unknown as AxiosInstance);
});

afterEach(() => {
  vi.useRealTimers();
  __setHttpForTests(null);
  restoreClipboard();
  vi.restoreAllMocks();
});

describe('useShareLink — success', () => {
  it('302 → clipboard.writeText вызван с presigned URL', async () => {
    getSpy.mockResolvedValueOnce({ status: 302, headers: { location: PRESIGNED } });
    const onSuccess = vi.fn();
    const { result } = renderHook(() => useShareLink({ onSuccess }), {
      wrapper: makeWrapper(),
    });

    act(() => {
      result.current.share(INPUT);
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(writeSpy).toHaveBeenCalledWith(PRESIGNED);
    expect(onSuccess).toHaveBeenCalledWith(
      expect.objectContaining({ location: PRESIGNED }),
      expect.objectContaining({ input: INPUT, copied: true }),
    );
    expect(result.current.copied).toBe(true);
  });

  it('copied автоматически сбрасывается через ~1500мс', async () => {
    getSpy.mockResolvedValueOnce({ status: 302, headers: { location: PRESIGNED } });
    const { result } = renderHook(() => useShareLink(), {
      wrapper: makeWrapper(),
    });

    act(() => {
      result.current.share(INPUT);
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.copied).toBe(true);

    // Real-timer-polling: useFakeTimers здесь нельзя — он заморозит setTimeout
    // внутри `waitFor`. Даём настоящему таймеру пройти ~1500мс и проверяем
    // сброс. timeout waitFor'а 2500мс > 1500мс-таймер в useCopy.
    await waitFor(() => expect(result.current.copied).toBe(false), { timeout: 2500 });
  });

  it('clipboard отклонил, но execCommand недоступен → copied=false, onSuccess c copied=false', async () => {
    getSpy.mockResolvedValueOnce({ status: 302, headers: { location: PRESIGNED } });
    writeSpy.mockRejectedValueOnce(new Error('denied'));
    // jsdom 24 не реализует document.execCommand; полифилим false-stub для теста.
    const execSpy = vi.fn(() => false);
    Object.defineProperty(document, 'execCommand', {
      value: execSpy,
      configurable: true,
      writable: true,
    });
    const onSuccess = vi.fn();
    const { result } = renderHook(() => useShareLink({ onSuccess }), {
      wrapper: makeWrapper(),
    });

    act(() => {
      result.current.share(INPUT);
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(execSpy).toHaveBeenCalledWith('copy');
    expect(onSuccess).toHaveBeenCalledWith(
      expect.objectContaining({ location: PRESIGNED }),
      expect.objectContaining({ copied: false }),
    );
    expect(result.current.copied).toBe(false);
  });
});

describe('useShareLink — error handling', () => {
  it('403 PERMISSION_DENIED → onError вызван, copy НЕ вызван', async () => {
    getSpy.mockRejectedValueOnce(orch('PERMISSION_DENIED', 403));
    const onError = vi.fn();
    const { result } = renderHook(() => useShareLink({ onError }), {
      wrapper: makeWrapper(),
    });

    act(() => {
      result.current.share(INPUT);
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(writeSpy).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledTimes(1);
    const [err, userMessage] = onError.mock.calls[0]!;
    expect(err).toMatchObject({ error_code: 'PERMISSION_DENIED' });
    expect(userMessage).toMatchObject({ title: expect.any(String) });
  });

  it('REQUEST_ABORTED → onError НЕ вызван', async () => {
    getSpy.mockRejectedValueOnce(orch('REQUEST_ABORTED'));
    const onError = vi.fn();
    const { result } = renderHook(() => useShareLink({ onError }), {
      wrapper: makeWrapper(),
    });

    act(() => {
      result.current.share(INPUT);
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(onError).not.toHaveBeenCalled();
  });
});
