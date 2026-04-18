// Локальный DI-контейнер для axios-инстанса (паттерн comparison-start/api/http.ts).
//
// Тесты переопределяют `httpInstance` на `createHttpClient(BASE)` с forced
// `adapter='fetch'` для MSW-перехвата.
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
