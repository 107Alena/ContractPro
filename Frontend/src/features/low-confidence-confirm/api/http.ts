// Локальный DI-контейнер для axios-инстанса (паттерн `processes/auth-flow/actions.ts`
// и `features/contract-upload/api/http.ts`).
//
// Зачем: MSW node-adapter ловит запросы через undici/http, а axios в jsdom по
// умолчанию ходит через XMLHttpRequest → MSW их не перехватит. Тесты
// переопределяют `httpInstance` на `createHttpClient(BASE)` с forced
// `adapter='http'`. В проде `httpInstance === http` (shared axios из @/shared/api).
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
