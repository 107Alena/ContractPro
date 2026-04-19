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

import { useSummary } from './use-summary';

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

describe('useSummary', () => {
  it('200 → data', async () => {
    getSpy.mockResolvedValueOnce({ data: { summary: 'ok' } });
    const { wrapper } = wrap();
    const { result } = renderHook(
      () => useSummary({ contractId: CONTRACT_ID, versionId: VERSION_ID }),
      { wrapper },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual({ summary: 'ok' });
  });

  it('contractId=undefined → запрос НЕ выполняется', async () => {
    const { wrapper } = wrap();
    renderHook(() => useSummary({ contractId: undefined, versionId: VERSION_ID }), { wrapper });
    await new Promise((r) => setTimeout(r, 10));
    expect(getSpy).not.toHaveBeenCalled();
  });

  it('404 ARTIFACT_NOT_FOUND → не ретраит', async () => {
    getSpy.mockRejectedValueOnce(
      new OrchestratorError({
        error_code: 'ARTIFACT_NOT_FOUND',
        message: 'not ready',
        status: 404,
      }),
    );
    const { wrapper } = wrap();
    const { result } = renderHook(
      () => useSummary({ contractId: CONTRACT_ID, versionId: VERSION_ID }),
      { wrapper },
    );
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(getSpy).toHaveBeenCalledTimes(1);
  });
});
