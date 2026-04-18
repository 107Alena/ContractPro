// useMe — useQuery на GET /users/me (§17.1, §17.3).
//
// queryKey: qk.me; staleTime 60s (профиль меняется редко).
// На 401 интерсептор в shared/api/client уже делает silent refresh (§5.4),
// поэтому хук не требует доп. обработки auth-ошибок.
import { useQuery, type UseQueryResult } from '@tanstack/react-query';

import { http, qk } from '@/shared/api';
import type { components } from '@/shared/api/openapi';

export type UserProfile = components['schemas']['UserProfile'];

const ENDPOINT = '/users/me';

async function fetchMe(signal?: AbortSignal): Promise<UserProfile> {
  const config = signal ? { signal } : undefined;
  const { data } = await http.get<UserProfile>(ENDPOINT, config);
  return data;
}

export function useMe(): UseQueryResult<UserProfile> {
  return useQuery({
    queryKey: qk.me,
    queryFn: ({ signal }) => fetchMe(signal),
    staleTime: 60_000,
  });
}

export { ENDPOINT as ME_ENDPOINT };
