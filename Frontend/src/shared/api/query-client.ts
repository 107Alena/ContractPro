import { QueryClient } from '@tanstack/react-query';

const DEFAULT_STALE_TIME_MS = 30_000;
const DEFAULT_QUERY_RETRIES = 1;

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: DEFAULT_STALE_TIME_MS,
        retry: DEFAULT_QUERY_RETRIES,
        refetchOnWindowFocus: false,
      },
    },
  });
}

export const queryClient: QueryClient = createQueryClient();

export function __resetQueryClientForTests(): void {
  queryClient.clear();
}
