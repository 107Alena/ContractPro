// Глобальный setup для vitest-тестов (FE-TASK-053/054).
// 1. Регистрирует @testing-library/jest-dom matchers (toBeInTheDocument и др.).
// 2. Полифилит `localStorage`/`sessionStorage` — в jsdom 24.1.3 + vitest 1.6.1
//    прототип этих объектов теряется при `populateGlobal`, что ломает любой код
//    с `zustand/middleware` persist.
// 3. Поднимает глобальный MSW-server (tests/msw/server.ts). Его базовый URL —
//    абсолютный `http://localhost/api/v1`, чтобы не конфликтовать с legacy-
//    тестами, использующими другой origin. `onUnhandledRequest: 'bypass'` —
//    запросы, не покрытые handlers, пропускаются (в jsdom они просто зафейлят
//    fetch сетевой ошибкой, что корректно для unit-тестов, не подключивших
//    моки вручную).
//
// Ключи localStorage сохраняются в памяти до конца теста; очистка — через
// `.clear()` в afterEach каждого теста.

import '@testing-library/jest-dom/vitest';

import { afterAll, afterEach, beforeAll } from 'vitest';

import { server } from '../tests/msw/server';

class MemoryStorage implements Storage {
  private store = new Map<string, string>();
  get length(): number {
    return this.store.size;
  }
  clear(): void {
    this.store.clear();
  }
  getItem(key: string): string | null {
    return this.store.has(key) ? (this.store.get(key) as string) : null;
  }
  key(index: number): string | null {
    return Array.from(this.store.keys())[index] ?? null;
  }
  removeItem(key: string): void {
    this.store.delete(key);
  }
  setItem(key: string, value: string): void {
    this.store.set(key, String(value));
  }
}

function ensureStorage(target: Window, name: 'localStorage' | 'sessionStorage'): void {
  const existing = target[name] as unknown;
  if (
    existing != null &&
    typeof (existing as Partial<Storage>).setItem === 'function' &&
    typeof (existing as Partial<Storage>).getItem === 'function'
  ) {
    return;
  }
  Object.defineProperty(target, name, {
    configurable: true,
    value: new MemoryStorage(),
  });
}

if (typeof window !== 'undefined') {
  ensureStorage(window, 'localStorage');
  ensureStorage(window, 'sessionStorage');
}

// MSW lifecycle. 'warn' — если тест забыл `server.use(...)` для URL вне
// default-handler-set, в логи пишется warning вместо молчаливого real-DNS
// lookup (последний давал 30с-timeout по умолчанию, P1 code-reviewer).
// Для Storybook — отдельный initialize({onUnhandledRequest:'bypass'}) в
// .storybook/preview.ts, чтобы CDN-запросы Chromatic не шумели.
beforeAll(() => server.listen({ onUnhandledRequest: 'warn' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
