// @vitest-environment jsdom
//
// Хук-тест useArchiveContract: оптимистичное обновление qk.contracts.byId +
// предиката ['contracts','list'], rollback при ошибке, invalidate в onSettled.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { AxiosInstance } from 'axios';
import { type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError, qk } from '@/shared/api';
import type { components } from '@/shared/api/openapi';

import { __setHttpForTests } from '../api/http';
import { useArchiveContract } from './use-archive-contract';

type ContractDetails = components['schemas']['ContractDetails'];
type ContractList = components['schemas']['ContractList'];

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const OTHER_ID = 'deadbeef-1111-2222-3333-444444444444';

const INITIAL_DETAILS: ContractDetails = {
  contract_id: CONTRACT_ID,
  title: 'Договор №1',
  status: 'ACTIVE',
  created_by_user_id: 'user-1',
  created_at: '2026-04-01T10:00:00Z',
  updated_at: '2026-04-10T10:00:00Z',
};

const INITIAL_LIST: ContractList = {
  items: [
    {
      contract_id: CONTRACT_ID,
      title: 'Договор №1',
      status: 'ACTIVE',
      current_version_number: 2,
      updated_at: '2026-04-10T10:00:00Z',
    },
    {
      contract_id: OTHER_ID,
      title: 'Другой договор',
      status: 'ACTIVE',
      current_version_number: 1,
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
  status: 'ARCHIVED' as const,
  current_version_number: 2,
  processing_status: 'READY' as const,
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
  // Засеиваем кэш, чтобы оптимистик-обновление было видно.
  qc.setQueryData(qk.contracts.byId(CONTRACT_ID), INITIAL_DETAILS);
  qc.setQueryData(qk.contracts.list({ page: 1, size: 20 }), INITIAL_LIST);
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

describe('useArchiveContract — optimistic success', () => {
  it('до ответа сервера byId и items[] патчатся status=ARCHIVED', async () => {
    // Держим запрос in-flight, пока проверяем оптимистик.
    let resolveRequest: (value: unknown) => void = () => undefined;
    postSpy.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveRequest = resolve;
        }),
    );
    const { wrapper, qc } = makeWrapper();
    const { result } = renderHook(() => useArchiveContract(), { wrapper });

    act(() => {
      result.current.archive({ contractId: CONTRACT_ID });
    });
    await waitFor(() => expect(result.current.isPending).toBe(true));

    const patchedById = qc.getQueryData<ContractDetails>(qk.contracts.byId(CONTRACT_ID));
    expect(patchedById?.status).toBe('ARCHIVED');
    // Остальные поля сохранились.
    expect(patchedById?.title).toBe('Договор №1');
    expect(patchedById?.created_by_user_id).toBe('user-1');

    const patchedList = qc.getQueryData<ContractList>(qk.contracts.list({ page: 1, size: 20 }));
    const targetItem = patchedList?.items?.find((i) => i.contract_id === CONTRACT_ID);
    const otherItem = patchedList?.items?.find((i) => i.contract_id === OTHER_ID);
    expect(targetItem?.status).toBe('ARCHIVED');
    // Другой договор не должен быть затронут.
    expect(otherItem?.status).toBe('ACTIVE');

    // Завершаем.
    resolveRequest({ data: SERVER_RESPONSE });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
  });

  it('onSuccess получает server-ответ; server updated_at перезаписывает локальный', async () => {
    postSpy.mockResolvedValueOnce({ data: SERVER_RESPONSE });
    const onSuccess = vi.fn();
    const { wrapper, qc } = makeWrapper();
    const { result } = renderHook(() => useArchiveContract({ onSuccess }), { wrapper });

    act(() => {
      result.current.archive({ contractId: CONTRACT_ID });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(onSuccess).toHaveBeenCalledWith(expect.objectContaining({ status: 'ARCHIVED' }));
    const finalById = qc.getQueryData<ContractDetails>(qk.contracts.byId(CONTRACT_ID));
    expect(finalById?.status).toBe('ARCHIVED');
    expect(finalById?.updated_at).toBe(SERVER_RESPONSE.updated_at);
  });

  it('onSettled инвалидирует qk.contracts.byId + list', async () => {
    postSpy.mockResolvedValueOnce({ data: SERVER_RESPONSE });
    const { wrapper, qc } = makeWrapper();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    const { result } = renderHook(() => useArchiveContract(), { wrapper });

    act(() => {
      result.current.archive({ contractId: CONTRACT_ID });
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

describe('useArchiveContract — rollback on error', () => {
  it('409 DOCUMENT_ARCHIVED → кэш восстановлен из snapshot, onError вызван', async () => {
    postSpy.mockRejectedValueOnce(orch('DOCUMENT_ARCHIVED', 'Документ уже архивирован', 409));
    const onError = vi.fn();
    const { wrapper, qc } = makeWrapper();
    const { result } = renderHook(() => useArchiveContract({ onError }), { wrapper });

    act(() => {
      result.current.archive({ contractId: CONTRACT_ID });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    // Snapshot восстановлен.
    const finalById = qc.getQueryData<ContractDetails>(qk.contracts.byId(CONTRACT_ID));
    expect(finalById).toEqual(INITIAL_DETAILS);

    const finalList = qc.getQueryData<ContractList>(qk.contracts.list({ page: 1, size: 20 }));
    expect(finalList?.items?.[0]?.status).toBe('ACTIVE');

    expect(onError).toHaveBeenCalledTimes(1);
    const [err, userMessage] = onError.mock.calls[0]!;
    expect(err).toMatchObject({ error_code: 'DOCUMENT_ARCHIVED', status: 409 });
    expect(userMessage.title).toBe('Документ уже архивирован');
  });

  it('500 INTERNAL_ERROR → rollback + onError с action=retry', async () => {
    postSpy.mockRejectedValueOnce(orch('INTERNAL_ERROR', 'Ошибка на сервере', 500));
    const onError = vi.fn();
    const { wrapper, qc } = makeWrapper();
    const { result } = renderHook(() => useArchiveContract({ onError }), { wrapper });

    act(() => {
      result.current.archive({ contractId: CONTRACT_ID });
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
    postSpy.mockRejectedValueOnce(orch('REQUEST_ABORTED', 'Отменено'));
    const onError = vi.fn();
    const { wrapper, qc } = makeWrapper();
    const { result } = renderHook(() => useArchiveContract({ onError }), { wrapper });

    act(() => {
      result.current.archive({ contractId: CONTRACT_ID });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(qc.getQueryData<ContractDetails>(qk.contracts.byId(CONTRACT_ID))).toEqual(
      INITIAL_DETAILS,
    );
    expect(onError).not.toHaveBeenCalled();
  });

  it('onSettled срабатывает и на ошибке (invalidate вызывается)', async () => {
    postSpy.mockRejectedValueOnce(orch('INTERNAL_ERROR', 'bang', 500));
    const { wrapper, qc } = makeWrapper();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    const { result } = renderHook(() => useArchiveContract(), { wrapper });

    act(() => {
      result.current.archive({ contractId: CONTRACT_ID });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(invalidateSpy).toHaveBeenCalled();
  });
});

describe('useArchiveContract — async API', () => {
  it('archiveAsync резолвит промис с server-ответом', async () => {
    postSpy.mockResolvedValueOnce({ data: SERVER_RESPONSE });
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useArchiveContract(), { wrapper });

    await expect(result.current.archiveAsync({ contractId: CONTRACT_ID })).resolves.toMatchObject({
      status: 'ARCHIVED',
    });
  });
});
