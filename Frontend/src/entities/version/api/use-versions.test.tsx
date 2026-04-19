// @vitest-environment jsdom
//
// Хук-тест useVersions: GET /contracts/{id}/versions, enabled=Boolean(id),
// queryKey = qk.contracts.versions(id). Мок `http.get` через vi.mock barrel.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react';
import { type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const { getSpy } = vi.hoisted(() => ({ getSpy: vi.fn() }));

vi.mock('@/shared/api', async (importActual) => {
  const actual = await importActual<typeof import('@/shared/api')>();
  return {
    ...actual,
    http: { get: getSpy },
  };
});

import { useVersions } from './use-versions';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';

const OK_RESPONSE = {
  items: [
    { version_id: 'v1', version_number: 1, processing_status: 'READY' },
    { version_id: 'v2', version_number: 2, processing_status: 'ANALYZING' },
  ],
  total: 2,
  page: 1,
  size: 20,
};

function makeWrapper(): {
  wrapper: (props: { children: ReactNode }) => JSX.Element;
  qc: QueryClient;
} {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
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

describe('useVersions', () => {
  it('200 → data, вызов GET /contracts/{id}/versions', async () => {
    getSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useVersions(CONTRACT_ID), { wrapper });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data).toEqual(OK_RESPONSE);
    expect(getSpy).toHaveBeenCalledWith(
      `/contracts/${CONTRACT_ID}/versions`,
      expect.objectContaining({ params: {} }),
    );
  });

  it('queryKey = qk.contracts.versions(id)', async () => {
    getSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const { wrapper, qc } = makeWrapper();
    renderHook(() => useVersions(CONTRACT_ID), { wrapper });

    await waitFor(() => {
      const cached = qc.getQueryData(['contracts', CONTRACT_ID, 'versions']);
      expect(cached).toBeDefined();
    });
  });

  it('id=undefined → запрос НЕ выполняется', async () => {
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useVersions(undefined), { wrapper });

    await new Promise<void>((resolve) => setTimeout(resolve, 10));
    expect(getSpy).not.toHaveBeenCalled();
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('передаёт params запроса в http.get', async () => {
    getSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const { wrapper } = makeWrapper();
    renderHook(() => useVersions(CONTRACT_ID, { page: 2, size: 10 }), { wrapper });

    await waitFor(() => expect(getSpy).toHaveBeenCalled());
    expect(getSpy).toHaveBeenCalledWith(
      `/contracts/${CONTRACT_ID}/versions`,
      expect.objectContaining({ params: { page: 2, size: 10 } }),
    );
  });
});
