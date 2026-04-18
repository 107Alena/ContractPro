// @vitest-environment jsdom
//
// Хук-тест useDiff: useQuery, retry-predicate (DIFF_NOT_FOUND → skip retry),
// enabled-переключатель, queryKey.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react';
import type { AxiosInstance } from 'axios';
import { type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api';

import { __setHttpForTests } from '../api/http';
import { useDiff } from './use-diff';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const BASE_ID = 'ba5e0000-aaaa-bbbb-cccc-111111111111';
const TARGET_ID = 'ta39e700-aaaa-bbbb-cccc-222222222222';

const OK_RESPONSE = {
  base_version_id: BASE_ID,
  target_version_id: TARGET_ID,
  text_diff_count: 1,
  structural_diff_count: 0,
  text_diffs: [{ type: 'added' as const, path: 'p.1', old_text: null, new_text: 'X' }],
  structural_diffs: [],
};

const INPUT = {
  contractId: CONTRACT_ID,
  baseVersionId: BASE_ID,
  targetVersionId: TARGET_ID,
};

function orch(code: string, status = 500): OrchestratorError {
  return new OrchestratorError({ error_code: code, message: 'm', status });
}

function makeWrapper(): {
  wrapper: (props: { children: ReactNode }) => JSX.Element;
  qc: QueryClient;
} {
  // retryDelay=0 — проверяем retry-predicate из useDiff, но без ожидания
  // экспоненциального backoff react-query (детерминированные тесты).
  const qc = new QueryClient({
    defaultOptions: { queries: { retryDelay: 0 } },
  });
  const wrapper = ({ children }: { children: ReactNode }): JSX.Element => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return { wrapper, qc };
}

let getSpy: ReturnType<typeof vi.fn>;

beforeEach(() => {
  getSpy = vi.fn();
  __setHttpForTests({ get: getSpy } as unknown as AxiosInstance);
});

afterEach(() => {
  __setHttpForTests(null);
});

describe('useDiff — success', () => {
  it('200 → data — narrowed VersionDiffResult', async () => {
    getSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useDiff(INPUT), { wrapper });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data).toEqual({
      baseVersionId: BASE_ID,
      targetVersionId: TARGET_ID,
      textDiffCount: 1,
      structuralDiffCount: 0,
      textDiffs: expect.arrayContaining([
        expect.objectContaining({ type: 'added', path: 'p.1' }),
      ]),
      structuralDiffs: [],
    });
    expect(getSpy).toHaveBeenCalledTimes(1);
  });

  it('queryKey = ["contracts", id, "diff", base, target]', async () => {
    getSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const { wrapper, qc } = makeWrapper();
    renderHook(() => useDiff(INPUT), { wrapper });

    await waitFor(() => {
      const cached = qc.getQueryData(['contracts', CONTRACT_ID, 'diff', BASE_ID, TARGET_ID]);
      expect(cached).toBeDefined();
    });
  });
});

describe('useDiff — retry predicate', () => {
  it('404 DIFF_NOT_FOUND → query НЕ ретраит (один запрос)', async () => {
    getSpy.mockRejectedValue(orch('DIFF_NOT_FOUND', 404));
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useDiff(INPUT), { wrapper });

    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(getSpy).toHaveBeenCalledTimes(1);
    expect(result.current.error).toMatchObject({ error_code: 'DIFF_NOT_FOUND' });
  });

  it('500 INTERNAL_ERROR → query ретраит до 2 раз (default=1 retry)', async () => {
    getSpy.mockRejectedValue(orch('INTERNAL_ERROR', 500));
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useDiff(INPUT), { wrapper });

    await waitFor(() => expect(result.current.isError).toBe(true));

    // 1 initial + 1 retry = 2 calls.
    expect(getSpy).toHaveBeenCalledTimes(2);
  });

  it('REQUEST_ABORTED → query НЕ ретраит (один запрос)', async () => {
    getSpy.mockRejectedValue(orch('REQUEST_ABORTED', 0));
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useDiff(INPUT), { wrapper });

    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(getSpy).toHaveBeenCalledTimes(1);
  });
});

describe('useDiff — enabled', () => {
  it('enabled=false → запрос НЕ выполняется', async () => {
    getSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useDiff(INPUT, { enabled: false }), { wrapper });

    // Ждём микротаск, потом проверяем что запрос не был сделан.
    await new Promise<void>((resolve) => setTimeout(resolve, 10));

    expect(getSpy).not.toHaveBeenCalled();
    expect(result.current.isLoading).toBe(false);
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('enabled=true (default) → запрос выполняется', async () => {
    getSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useDiff(INPUT), { wrapper });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(getSpy).toHaveBeenCalledTimes(1);
  });
});
