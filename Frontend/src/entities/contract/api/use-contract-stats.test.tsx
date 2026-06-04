// @vitest-environment jsdom
//
// Хук-тест useContractStats: GET /contracts/stats, queryKey = qk.contracts.stats.
// Плюс unit на чистый inProgressCount (derive «в работе»). Мок http.get через
// vi.mock barrel'а shared/api (как в use-contract.test.tsx).
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

import { type ContractStats, inProgressCount, useContractStats } from './use-contract-stats';

function makeStats(partial: Partial<ContractStats['by_processing_status']>): ContractStats {
  return {
    total: 0,
    by_processing_status: {
      uploaded: 0,
      queued: 0,
      processing: 0,
      analyzing: 0,
      awaiting_user_input: 0,
      generating_reports: 0,
      ready: 0,
      partially_failed: 0,
      failed: 0,
      rejected: 0,
      not_started: 0,
      ...partial,
    },
  };
}

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

describe('useContractStats', () => {
  it('200 → data, вызов GET /contracts/stats', async () => {
    const stats = makeStats({ analyzing: 1, ready: 2 });
    stats.total = 3;
    getSpy.mockResolvedValueOnce({ data: stats });
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useContractStats(), { wrapper });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data).toEqual(stats);
    expect(getSpy).toHaveBeenCalledWith('/contracts/stats', expect.any(Object));
  });

  it('queryKey = qk.contracts.stats', async () => {
    getSpy.mockResolvedValueOnce({ data: makeStats({}) });
    const { wrapper, qc } = makeWrapper();
    renderHook(() => useContractStats(), { wrapper });

    await waitFor(() => {
      expect(qc.getQueryData(['contracts', 'stats'])).toBeDefined();
    });
  });
});

describe('inProgressCount', () => {
  it('undefined stats → undefined', () => {
    expect(inProgressCount(undefined)).toBeUndefined();
  });

  it('суммирует pending + in_progress (uploaded+queued+processing+analyzing+generating_reports)', () => {
    const stats = makeStats({
      uploaded: 1,
      queued: 2,
      processing: 3,
      analyzing: 4,
      generating_reports: 5,
      // НЕ в работе:
      awaiting_user_input: 9,
      ready: 9,
      failed: 9,
      rejected: 9,
      partially_failed: 9,
      not_started: 9,
    });
    expect(inProgressCount(stats)).toBe(1 + 2 + 3 + 4 + 5);
  });

  it('все терминальные/awaiting → 0', () => {
    const stats = makeStats({ ready: 7, awaiting_user_input: 3, failed: 2, not_started: 1 });
    expect(inProgressCount(stats)).toBe(0);
  });
});
