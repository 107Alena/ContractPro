// @vitest-environment jsdom
//
// Hook-тест useResults: GET /contracts/{id}/versions/{vid}/results,
// enabled = Boolean(id && vid), queryKey = qk.contracts.results(id, vid).
// Мок http.get через vi.mock — ResultPage потребляет данные напрямую.
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

import { useResults } from './use-results';

const CONTRACT_ID = 'c1';
const VERSION_ID = 'v1';

const OK_RESPONSE = {
  version_id: VERSION_ID,
  status: 'READY' as const,
  summary: 'Краткое резюме',
  risks: [],
  recommendations: [],
};

function makeWrapper(): {
  wrapper: (props: { children: ReactNode }) => JSX.Element;
  qc: QueryClient;
} {
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

describe('useResults', () => {
  it('200 → data, вызов GET /results', async () => {
    getSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const { wrapper } = makeWrapper();
    const { result } = renderHook(
      () => useResults({ contractId: CONTRACT_ID, versionId: VERSION_ID }),
      { wrapper },
    );

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual(OK_RESPONSE);
    expect(getSpy).toHaveBeenCalledWith(
      `/contracts/${CONTRACT_ID}/versions/${VERSION_ID}/results`,
      expect.any(Object),
    );
  });

  it('contractId/versionId=undefined → запрос НЕ выполняется (enabled=false)', async () => {
    const { wrapper } = makeWrapper();
    renderHook(() => useResults({ contractId: undefined, versionId: VERSION_ID }), { wrapper });
    await new Promise((r) => setTimeout(r, 10));
    expect(getSpy).not.toHaveBeenCalled();
  });

  it('options.enabled=false → запрос НЕ выполняется', async () => {
    const { wrapper } = makeWrapper();
    renderHook(
      () => useResults({ contractId: CONTRACT_ID, versionId: VERSION_ID }, { enabled: false }),
      { wrapper },
    );
    await new Promise((r) => setTimeout(r, 10));
    expect(getSpy).not.toHaveBeenCalled();
  });

  it('404 ARTIFACT_NOT_FOUND → query НЕ ретраит', async () => {
    getSpy.mockRejectedValueOnce(
      new OrchestratorError({
        error_code: 'ARTIFACT_NOT_FOUND',
        message: 'not ready',
        status: 404,
      }),
    );
    const { wrapper } = makeWrapper();
    const { result } = renderHook(
      () => useResults({ contractId: CONTRACT_ID, versionId: VERSION_ID }),
      { wrapper },
    );
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(getSpy).toHaveBeenCalledTimes(1);
  });

  it('500 INTERNAL_ERROR → query ретраит 1 раз', async () => {
    getSpy.mockRejectedValue(
      new OrchestratorError({
        error_code: 'INTERNAL_ERROR',
        message: 'internal',
        status: 500,
      }),
    );
    const { wrapper } = makeWrapper();
    const { result } = renderHook(
      () => useResults({ contractId: CONTRACT_ID, versionId: VERSION_ID }),
      { wrapper },
    );
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(getSpy).toHaveBeenCalledTimes(2);
  });
});
