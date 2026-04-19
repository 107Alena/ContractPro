// Локальный DI-контейнер для axios-инстанса.
//
// Тесты переопределяют `httpInstance` на `createHttpClient(BASE)` с forced
// `adapter='fetch'` для MSW-перехвата (см. version-recheck/api/http.ts для
// полной мотивации).
import type { AxiosInstance } from 'axios';

import { http } from '@/shared/api';

let httpInstance: AxiosInstance = http;

export function getHttpInstance(): AxiosInstance {
  return httpInstance;
}

/** @internal Только для тестов. `null` → возврат к shared `http`. */
export function __setHttpForTests(instance: AxiosInstance | null): void {
  httpInstance = instance ?? http;
}
