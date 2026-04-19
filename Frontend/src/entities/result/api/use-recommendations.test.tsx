// @vitest-environment jsdom
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

import { useRecommendations } from './use-recommendations';

const CONTRACT_ID = 'c1';
const VERSION_ID = 'v1';

function wrap(): { wrapper: (p: { children: ReactNode }) => JSX.Element } {
  const qc = new QueryClient({ defaultOptions: { queries: { retryDelay: 0, gcTime: 0 } } });
  const wrapper = ({ children }: { children: ReactNode }): JSX.Element => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return { wrapper };
}

beforeEach(() => {
  getSpy.mockReset();
});
afterEach(() => {
  vi.clearAllMocks();
});

describe('useRecommendations', () => {
  it('200 → data', async () => {
    getSpy.mockResolvedValueOnce({ data: { items: [] } });
    const { wrapper } = wrap();
    const { result } = renderHook(
      () => useRecommendations({ contractId: CONTRACT_ID, versionId: VERSION_ID }),
      { wrapper },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual({ items: [] });
  });

  it('enabled=false → запрос НЕ выполняется', async () => {
    const { wrapper } = wrap();
    renderHook(
      () =>
        useRecommendations({ contractId: CONTRACT_ID, versionId: VERSION_ID }, { enabled: false }),
      { wrapper },
    );
    await new Promise((r) => setTimeout(r, 10));
    expect(getSpy).not.toHaveBeenCalled();
  });

  it('403 PERMISSION_DENIED → не ретраит', async () => {
    getSpy.mockRejectedValueOnce(
      new OrchestratorError({
        error_code: 'PERMISSION_DENIED',
        message: 'forbidden',
        status: 403,
      }),
    );
    const { wrapper } = wrap();
    const { result } = renderHook(
      () => useRecommendations({ contractId: CONTRACT_ID, versionId: VERSION_ID }),
      { wrapper },
    );
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(getSpy).toHaveBeenCalledTimes(1);
  });
});
