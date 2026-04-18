// Глобальный setup для vitest-тестов (FE-TASK-053 расширит под jest-dom matchers,
// MSW-реестр и coverage-шипсы). На момент FE-TASK-032 полифилим `localStorage` /
// `sessionStorage`: в jsdom 24.1.3 + vitest 1.6.1 прототип этих объектов теряется
// при `populateGlobal` — `localStorage.setItem` возвращает `undefined` и ломает
// любой код с `zustand/middleware` persist. Полифил включается только если
// нативная реализация реально нерабочая, иначе уходит из пути (no-op).
//
// Ключи сохраняются в памяти до конца теста; очистка — через `.clear()` в
// afterEach каждого теста. Не пытаемся реализовывать StorageEvent — модулям
// приложения он не требуется (single-tab SPA).

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
