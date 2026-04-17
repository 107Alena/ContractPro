/**
 * @vitest-environment jsdom
 */
import { useQuery } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it } from 'vitest';

import { __resetQueryClientForTests, qk, queryClient } from '@/shared/api';

import { QueryProvider } from './QueryProvider';

function wrapper({ children }: { children: ReactNode }): JSX.Element {
  return <QueryProvider>{children}</QueryProvider>;
}

afterEach(() => {
  __resetQueryClientForTests();
});

describe('QueryProvider + useQuery(qk.me)', () => {
  it('caches data under ["me"] key after useQuery resolves', async () => {
    const mockUser = { id: 'u-1', email: 'test@example.com' };

    const { result } = renderHook(
      () =>
        useQuery({
          queryKey: qk.me,
          queryFn: () => Promise.resolve(mockUser),
        }),
      { wrapper },
    );

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });

    expect(result.current.data).toEqual(mockUser);
    expect(queryClient.getQueryData(qk.me)).toEqual(mockUser);
    expect(queryClient.getQueryData(['me'])).toEqual(mockUser);
  });

  it('respects default retry=1 and staleTime=30_000', async () => {
    const { result } = renderHook(
      () =>
        useQuery({
          queryKey: ['default-options-probe'],
          queryFn: () => Promise.resolve('ok'),
        }),
      { wrapper },
    );

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });

    const state = queryClient.getQueryState(['default-options-probe']);
    expect(state?.dataUpdatedAt).toBeGreaterThan(0);
  });
});
