import { QueryClient } from '@tanstack/react-query';
import { afterEach, describe, expect, it } from 'vitest';

import { __resetQueryClientForTests, createQueryClient, queryClient } from './query-client';

afterEach(() => {
  __resetQueryClientForTests();
});

describe('queryClient', () => {
  it('is a QueryClient instance (singleton)', () => {
    expect(queryClient).toBeInstanceOf(QueryClient);
  });

  it('has default options from §4.3: staleTime=30_000, retry=1, refetchOnWindowFocus=false', () => {
    const defaults = queryClient.getDefaultOptions();
    expect(defaults.queries?.staleTime).toBe(30_000);
    expect(defaults.queries?.retry).toBe(1);
    expect(defaults.queries?.refetchOnWindowFocus).toBe(false);
  });

  it('createQueryClient returns independent instances', () => {
    const a = createQueryClient();
    const b = createQueryClient();
    expect(a).not.toBe(b);
    a.setQueryData(['k'], 1);
    expect(b.getQueryData(['k'])).toBeUndefined();
  });

  it('__resetQueryClientForTests clears the singleton cache', () => {
    queryClient.setQueryData(['me'], { id: 'u-1' });
    expect(queryClient.getQueryData(['me'])).toEqual({ id: 'u-1' });
    __resetQueryClientForTests();
    expect(queryClient.getQueryData(['me'])).toBeUndefined();
  });
});
