// @vitest-environment jsdom
//
// Хук-тест useStartComparison: React-слой (useMutation, инвалидация
// qk.contracts.diff, 409 VERSION_STILL_PROCESSING через toUserMessage).
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { AxiosInstance } from 'axios';
import { type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api';

import { __setHttpForTests } from '../api/http';
import { useStartComparison } from './use-start-comparison';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const BASE_ID = 'ba5e0000-aaaa-bbbb-cccc-111111111111';
const TARGET_ID = 'ta39e700-aaaa-bbbb-cccc-222222222222';

const OK_RESPONSE = {
  job_id: 'j0b00000-5555-6666-7777-888888888888',
  status: 'QUEUED' as const,
};

const INPUT = {
  contractId: CONTRACT_ID,
  baseVersionId: BASE_ID,
  targetVersionId: TARGET_ID,
};

function orch(code: string, message = 'msg', status?: number): OrchestratorError {
  return new OrchestratorError(
    status !== undefined
      ? { error_code: code, message, status }
      : { error_code: code, message },
  );
}

function makeWrapper(): {
  wrapper: (props: { children: ReactNode }) => JSX.Element;
  qc: QueryClient;
} {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  const wrapper = ({ children }: { children: ReactNode }): JSX.Element => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return { wrapper, qc };
}

let postSpy: ReturnType<typeof vi.fn>;

beforeEach(() => {
  postSpy = vi.fn();
  __setHttpForTests({ post: postSpy } as unknown as AxiosInstance);
});

afterEach(() => {
  __setHttpForTests(null);
});

describe('useStartComparison — success + invalidation', () => {
  it('202 → onSuccess c narrowed-response {jobId, status}', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const onSuccess = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useStartComparison({ onSuccess }), { wrapper });

    act(() => {
      result.current.startComparison(INPUT);
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(onSuccess).toHaveBeenCalledWith(
      expect.objectContaining({
        jobId: OK_RESPONSE.job_id,
        status: 'QUEUED',
      }),
    );
  });

  it('инвалидация qk.contracts.diff(id, base, target)', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const { wrapper, qc } = makeWrapper();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    const { result } = renderHook(() => useStartComparison(), { wrapper });

    act(() => {
      result.current.startComparison(INPUT);
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const invalidatedKeys = invalidateSpy.mock.calls.map((c) =>
      (c[0] as { queryKey: unknown[] }).queryKey,
    );
    expect(invalidatedKeys).toEqual(
      expect.arrayContaining([
        ['contracts', CONTRACT_ID, 'diff', BASE_ID, TARGET_ID],
      ]),
    );
  });

  it('startComparisonAsync резолвит промис с response', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useStartComparison(), { wrapper });

    const promise = result.current.startComparisonAsync(INPUT);
    await expect(promise).resolves.toMatchObject({
      jobId: OK_RESPONSE.job_id,
      status: 'QUEUED',
    });
  });
});

describe('useStartComparison — error handling', () => {
  it('409 VERSION_STILL_PROCESSING → onError получает title+hint из ERROR_UX', async () => {
    postSpy.mockRejectedValueOnce(
      orch('VERSION_STILL_PROCESSING', 'Версия ещё обрабатывается', 409),
    );
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useStartComparison({ onError }), { wrapper });

    act(() => {
      result.current.startComparison(INPUT);
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(onError).toHaveBeenCalledTimes(1);
    const [err, userMessage] = onError.mock.calls[0]!;
    expect(err).toMatchObject({ error_code: 'VERSION_STILL_PROCESSING', status: 409 });
    expect(userMessage).toMatchObject({
      title: 'Версия ещё обрабатывается',
      hint: 'Дождитесь завершения.',
    });
  });

  it('404 VERSION_NOT_FOUND → onError получает title из catalog', async () => {
    postSpy.mockRejectedValueOnce(orch('VERSION_NOT_FOUND', 'Версия не найдена', 404));
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useStartComparison({ onError }), { wrapper });

    act(() => {
      result.current.startComparison(INPUT);
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(onError).toHaveBeenCalledWith(
      expect.objectContaining({ error_code: 'VERSION_NOT_FOUND' }),
      expect.objectContaining({ title: 'Версия не найдена' }),
    );
  });

  it('500 INTERNAL_ERROR → onError получает title из catalog', async () => {
    postSpy.mockRejectedValueOnce(orch('INTERNAL_ERROR', 'server msg', 500));
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useStartComparison({ onError }), { wrapper });

    act(() => {
      result.current.startComparison(INPUT);
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(onError).toHaveBeenCalledWith(
      expect.objectContaining({ error_code: 'INTERNAL_ERROR' }),
      expect.objectContaining({ action: 'retry' }),
    );
  });

  it('REQUEST_ABORTED → onError НЕ вызван', async () => {
    postSpy.mockRejectedValueOnce(orch('REQUEST_ABORTED', 'Запрос отменён'));
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useStartComparison({ onError }), { wrapper });

    act(() => {
      result.current.startComparison(INPUT);
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(onError).not.toHaveBeenCalled();
  });

  it('onError НЕ вызван на success', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useStartComparison({ onError }), { wrapper });

    act(() => {
      result.current.startComparison(INPUT);
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(onError).not.toHaveBeenCalled();
  });
});
