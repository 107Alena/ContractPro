// @vitest-environment jsdom
//
// Хук-тест useRecheckVersion: React-слой (useMutation, инвалидация двух
// query-keys, 409 VERSION_STILL_PROCESSING через toUserMessage).
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { AxiosInstance } from 'axios';
import { type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api';

import { __setHttpForTests } from '../api/http';
import { useRecheckVersion } from './use-recheck-version';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const VERSION_ID = 'v1ee0000-aaaa-bbbb-cccc-dddddddddddd';

const OK_RESPONSE = {
  contract_id: CONTRACT_ID,
  version_id: VERSION_ID,
  version_number: 2,
  job_id: 'j0b00000-5555-6666-7777-888888888888',
  status: 'QUEUED' as const,
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

describe('useRecheckVersion — success + invalidation', () => {
  it('202 → onSuccess с narrowed-response', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const onSuccess = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useRecheckVersion({ onSuccess }), { wrapper });

    act(() => {
      result.current.recheck({ contractId: CONTRACT_ID, versionId: VERSION_ID });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(onSuccess).toHaveBeenCalledWith(
      expect.objectContaining({
        contractId: CONTRACT_ID,
        versionId: VERSION_ID,
        versionNumber: 2,
        status: 'QUEUED',
      }),
    );
  });

  it('инвалидация qk.contracts.versions(id) + qk.contracts.status(id, vid)', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const { wrapper, qc } = makeWrapper();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    const { result } = renderHook(() => useRecheckVersion(), { wrapper });

    act(() => {
      result.current.recheck({ contractId: CONTRACT_ID, versionId: VERSION_ID });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const invalidatedKeys = invalidateSpy.mock.calls.map((c) =>
      (c[0] as { queryKey: unknown[] }).queryKey,
    );
    expect(invalidatedKeys).toEqual(
      expect.arrayContaining([
        ['contracts', CONTRACT_ID, 'versions'],
        ['contracts', CONTRACT_ID, 'versions', VERSION_ID, 'status'],
      ]),
    );
  });

  it('recheckAsync резолвит промис с response', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useRecheckVersion(), { wrapper });

    const promise = result.current.recheckAsync({ contractId: CONTRACT_ID, versionId: VERSION_ID });
    await expect(promise).resolves.toMatchObject({ versionId: VERSION_ID, status: 'QUEUED' });
  });
});

describe('useRecheckVersion — error handling', () => {
  it('409 VERSION_STILL_PROCESSING → onError получает title+hint из ERROR_UX', async () => {
    postSpy.mockRejectedValueOnce(
      orch('VERSION_STILL_PROCESSING', 'Версия ещё обрабатывается', 409),
    );
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useRecheckVersion({ onError }), { wrapper });

    act(() => {
      result.current.recheck({ contractId: CONTRACT_ID, versionId: VERSION_ID });
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

  it('500 INTERNAL_ERROR → onError получает title из server message', async () => {
    postSpy.mockRejectedValueOnce(orch('INTERNAL_ERROR', 'Ошибка на сервере', 500));
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useRecheckVersion({ onError }), { wrapper });

    act(() => {
      result.current.recheck({ contractId: CONTRACT_ID, versionId: VERSION_ID });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(onError).toHaveBeenCalledWith(
      expect.objectContaining({ error_code: 'INTERNAL_ERROR' }),
      expect.objectContaining({ title: 'Ошибка на сервере' }),
    );
  });

  it('REQUEST_ABORTED → onError НЕ вызван', async () => {
    postSpy.mockRejectedValueOnce(orch('REQUEST_ABORTED', 'Запрос отменён'));
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useRecheckVersion({ onError }), { wrapper });

    act(() => {
      result.current.recheck({ contractId: CONTRACT_ID, versionId: VERSION_ID });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(onError).not.toHaveBeenCalled();
  });

  it('onError НЕ вызван на success', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const onError = vi.fn();
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useRecheckVersion({ onError }), { wrapper });

    act(() => {
      result.current.recheck({ contractId: CONTRACT_ID, versionId: VERSION_ID });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(onError).not.toHaveBeenCalled();
  });
});
