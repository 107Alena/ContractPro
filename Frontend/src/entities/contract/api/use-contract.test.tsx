// @vitest-environment jsdom
//
// Хук-тест useContract: GET /contracts/{id}, enabled=Boolean(id), queryKey =
// qk.contracts.byId(id). Мок `http.get` через vi.mock — локального http-модуля
// на уровне entities нет (в отличие от features), поэтому прокалываем barrel.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react';
import { type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api';

const { getSpy } = vi.hoisted(() => ({ getSpy: vi.fn() }));

vi.mock('@/shared/api', async (importActual) => {
  const actual = await importActual<typeof import('@/shared/api')>();
  return {
    ...actual,
    http: { get: getSpy },
  };
});

import { useContract } from './use-contract';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';

const OK_RESPONSE = {
  contract_id: CONTRACT_ID,
  title: 'Договор',
  status: 'ACTIVE',
  current_version: { version_id: 'v1', version_number: 1, processing_status: 'READY' },
};

function makeWrapper(): {
  wrapper: (props: { children: ReactNode }) => JSX.Element;
  qc: QueryClient;
} {
  // retryDelay=0 — retry-предикат useContract сам по себе работает,
  // но без этого waitFor ждёт exponential-backoff TanStack (нестабильно).
  const qc = new QueryClient({
    defaultOptions: { queries: { retryDelay: 0, gcTime: 0 } },
  });
  const wrapper = ({ children }: { children: ReactNode }): JSX.Element => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return { wrapper, qc };
}

beforeEach(() => {
  getSpy.mockReset();
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('useContract', () => {
  it('200 → data, вызов GET /contracts/{id}', async () => {
    getSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useContract(CONTRACT_ID), { wrapper });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data).toEqual(OK_RESPONSE);
    expect(getSpy).toHaveBeenCalledWith(`/contracts/${CONTRACT_ID}`, expect.any(Object));
  });

  it('queryKey = qk.contracts.byId(id)', async () => {
    getSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const { wrapper, qc } = makeWrapper();
    renderHook(() => useContract(CONTRACT_ID), { wrapper });

    await waitFor(() => {
      const cached = qc.getQueryData(['contracts', CONTRACT_ID]);
      expect(cached).toBeDefined();
    });
  });

  it('id=undefined → запрос НЕ выполняется (enabled=false)', async () => {
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useContract(undefined), { wrapper });

    await new Promise<void>((resolve) => setTimeout(resolve, 10));
    expect(getSpy).not.toHaveBeenCalled();
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('options.enabled=false → запрос НЕ выполняется даже при валидном id', async () => {
    const { wrapper } = makeWrapper();
    renderHook(() => useContract(CONTRACT_ID, { enabled: false }), { wrapper });

    await new Promise<void>((resolve) => setTimeout(resolve, 10));
    expect(getSpy).not.toHaveBeenCalled();
  });

  it('404 CONTRACT_NOT_FOUND → query НЕ ретраит (один запрос)', async () => {
    getSpy.mockRejectedValue(
      new OrchestratorError({
        error_code: 'CONTRACT_NOT_FOUND',
        message: 'not found',
        status: 404,
      }),
    );
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useContract(CONTRACT_ID), { wrapper });

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(getSpy).toHaveBeenCalledTimes(1);
  });

  it('500 INTERNAL_ERROR → query ретраит один раз (1 initial + 1 retry = 2)', async () => {
    getSpy.mockRejectedValue(
      new OrchestratorError({
        error_code: 'INTERNAL_ERROR',
        message: 'internal',
        status: 500,
      }),
    );
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useContract(CONTRACT_ID), { wrapper });

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(getSpy).toHaveBeenCalledTimes(2);
  });
});
