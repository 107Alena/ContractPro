// @vitest-environment jsdom
//
// Хук-тест useDeleteContract: оптимистичное обновление byId (status=DELETED) +
// фильтрация item из items[] (decrement total), rollback при ошибке,
// invalidate в onSettled.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { AxiosInstance } from 'axios';
import { type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError, qk } from '@/shared/api';
import type { components } from '@/shared/api/openapi';

import { __setHttpForTests } from '../api/http';
import { useDeleteContract } from './use-delete-contract';

type ContractDetails = components['schemas']['ContractDetails'];
type ContractList = components['schemas']['ContractList'];

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const OTHER_ID = 'deadbeef-1111-2222-3333-444444444444';

const INITIAL_DETAILS: ContractDetails = {
  contract_id: CONTRACT_ID,
  title: 'Договор №1',
  status: 'ACTIVE',
  created_at: '2026-04-01T10:00:00Z',
  updated_at: '2026-04-10T10:00:00Z',
};

const INITIAL_LIST: ContractList = {
  items: [
    {
      contract_id: CONTRACT_ID,
      title: 'Договор №1',
      status: 'ACTIVE',
      updated_at: '2026-04-10T10:00:00Z',
    },
    {
      contract_id: OTHER_ID,
      title: 'Другой',
      status: 'ACTIVE',
      updated_at: '2026-04-11T10:00:00Z',
    },
  ],
  total: 2,
  page: 1,
  size: 20,
};

const SERVER_RESPONSE = {
  contract_id: CONTRACT_ID,
  title: 'Договор №1',
  status: 'DELETED' as const,
  created_at: '2026-04-01T10:00:00Z',
  updated_at: '2026-04-19T12:00:00Z',
};

function orch(code: string, message = 'msg', status?: number): OrchestratorError {
  return new OrchestratorError(
    status !== undefined ? { error_code: code, message, status } : { error_code: code, message },
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
  qc.setQueryData(qk.contracts.byId(CONTRACT_ID), INITIAL_DETAILS);
  qc.setQueryData(qk.contracts.list({ page: 1, size: 20 }), INITIAL_LIST);
  const wrapper = ({ children }: { children: ReactNode }): JSX.Element => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return { wrapper, qc };
}

let deleteSpy: ReturnType<typeof vi.fn>;

beforeEach(() => {
  deleteSpy = vi.fn();
  __setHttpForTests({ delete: deleteSpy } as unknown as AxiosInstance);
});

afterEach(() => {
  __setHttpForTests(null);
});

describe('useDeleteContract — optimistic success', () => {
  it('до ответа byId status=DELETED, item убран из items[], total уменьшен', async () => {
    let resolveRequest: (value: unknown) => void = () => undefined;
    deleteSpy.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveRequest = resolve;
        }),
    );
    const { wrapper, qc } = makeWrapper();
    const { result } = renderHook(() => useDeleteContract(), { wrapper });

    act(() => {
      result.current.remove({ contractId: CONTRACT_ID });
    });
    await waitFor(() => expect(result.current.isPending).toBe(true));

    const patchedById = qc.getQueryData<ContractDetails>(qk.contracts.byId(CONTRACT_ID));
    expect(patchedById?.status).toBe('DELETED');
    // Ресурс НЕ удалён из кэша.
    expect(patchedById).toBeDefined();

    const patchedList = qc.getQueryData<ContractList>(qk.contracts.list({ page: 1, size: 20 }));
    expect(patchedList?.items).toHaveLength(1);
    expect(patchedList?.items?.[0]?.contract_id).toBe(OTHER_ID);
    expect(patchedList?.total).toBe(1);

    resolveRequest({ data: SERVER_RESPONSE });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
  });

  it('onSuccess получает server-ответ; server updated_at перезаписывает локальный', async () => {
    deleteSpy.mockResolvedValueOnce({ data: SERVER_RESPONSE });
    const onSuccess = vi.fn();
    const { wrapper, qc } = makeWrapper();
    const { result } = renderHook(() => useDeleteContract({ onSuccess }), { wrapper });

    act(() => {
      result.current.remove({ contractId: CONTRACT_ID });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(onSuccess).toHaveBeenCalledWith(expect.objectContaining({ status: 'DELETED' }));
    const finalById = qc.getQueryData<ContractDetails>(qk.contracts.byId(CONTRACT_ID));
    expect(finalById?.updated_at).toBe(SERVER_RESPONSE.updated_at);
  });

  it('onSettled инвалидирует qk.contracts.byId + list', async () => {
    deleteSpy.mockResolvedValueOnce({ data: SERVER_RESPONSE });
    const { wrapper, qc } = makeWrapper();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    const { result } = renderHook(() => useDeleteContract(), { wrapper });

    act(() => {
      result.current.remove({ contractId: CONTRACT_ID });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const invalidatedKeys = invalidateSpy.mock.calls.map(
      (c) => (c[0] as { queryKey: unknown[] }).queryKey,
    );
    expect(invalidatedKeys).toEqual(
      expect.arrayContaining([
        ['contracts', CONTRACT_ID],
        ['contracts', 'list'],
      ]),
    );
  });
});

describe('useDeleteContract — rollback on error', () => {
  it('409 DOCUMENT_DELETED → rollback byId + items[] + total', async () => {
    deleteSpy.mockRejectedValueOnce(orch('DOCUMENT_DELETED', 'Уже удалён', 409));
    const onError = vi.fn();
    const { wrapper, qc } = makeWrapper();
    const { result } = renderHook(() => useDeleteContract({ onError }), { wrapper });

    act(() => {
      result.current.remove({ contractId: CONTRACT_ID });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(qc.getQueryData<ContractDetails>(qk.contracts.byId(CONTRACT_ID))).toEqual(
      INITIAL_DETAILS,
    );
    const restored = qc.getQueryData<ContractList>(qk.contracts.list({ page: 1, size: 20 }));
    expect(restored?.items).toHaveLength(2);
    expect(restored?.total).toBe(2);

    expect(onError).toHaveBeenCalledWith(
      expect.objectContaining({ error_code: 'DOCUMENT_DELETED', status: 409 }),
      expect.anything(),
    );
  });

  it('500 INTERNAL_ERROR → rollback + onError с action=retry', async () => {
    deleteSpy.mockRejectedValueOnce(orch('INTERNAL_ERROR', 'bang', 500));
    const onError = vi.fn();
    const { wrapper, qc } = makeWrapper();
    const { result } = renderHook(() => useDeleteContract({ onError }), { wrapper });

    act(() => {
      result.current.remove({ contractId: CONTRACT_ID });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(qc.getQueryData<ContractDetails>(qk.contracts.byId(CONTRACT_ID))).toEqual(
      INITIAL_DETAILS,
    );
    expect(onError).toHaveBeenCalledWith(
      expect.objectContaining({ error_code: 'INTERNAL_ERROR' }),
      expect.objectContaining({ action: 'retry' }),
    );
  });

  it('REQUEST_ABORTED → rollback без вызова onError', async () => {
    deleteSpy.mockRejectedValueOnce(orch('REQUEST_ABORTED', 'Отменено'));
    const onError = vi.fn();
    const { wrapper, qc } = makeWrapper();
    const { result } = renderHook(() => useDeleteContract({ onError }), { wrapper });

    act(() => {
      result.current.remove({ contractId: CONTRACT_ID });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(qc.getQueryData<ContractDetails>(qk.contracts.byId(CONTRACT_ID))).toEqual(
      INITIAL_DETAILS,
    );
    expect(onError).not.toHaveBeenCalled();
  });
});

describe('useDeleteContract — async API', () => {
  it('removeAsync резолвит промис с server-ответом', async () => {
    deleteSpy.mockResolvedValueOnce({ data: SERVER_RESPONSE });
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useDeleteContract(), { wrapper });

    await expect(result.current.removeAsync({ contractId: CONTRACT_ID })).resolves.toMatchObject({
      status: 'DELETED',
    });
  });
});
