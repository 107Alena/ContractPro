// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';

import {
  __REFRESH_STORAGE_KEY,
  clearRefreshToken,
  getRefreshToken,
  setRefreshToken,
} from './refresh-token-storage';

describe('refresh-token-storage', () => {
  afterEach(() => {
    window.sessionStorage.clear();
  });

  it('roundtrip: setRefreshToken → getRefreshToken возвращает тот же токен', () => {
    setRefreshToken('rt.eyJhbGciOiJIUzI1NiJ9.abc.def');
    expect(getRefreshToken()).toBe('rt.eyJhbGciOiJIUzI1NiJ9.abc.def');
  });

  it('пустое хранилище → getRefreshToken возвращает null', () => {
    expect(getRefreshToken()).toBeNull();
  });

  it('хранит токен под фиксированным ключом, значение не совпадает с plain (obfuscation)', () => {
    setRefreshToken('plain-token-abc');
    const raw = window.sessionStorage.getItem(__REFRESH_STORAGE_KEY);
    expect(raw).not.toBeNull();
    // Base64 + XOR ≠ исходник. Гарантирует не-plain-text хранение.
    expect(raw).not.toContain('plain-token-abc');
  });

  it('clearRefreshToken удаляет запись', () => {
    setRefreshToken('tok');
    clearRefreshToken();
    expect(getRefreshToken()).toBeNull();
    expect(window.sessionStorage.getItem(__REFRESH_STORAGE_KEY)).toBeNull();
  });

  it('повреждённое значение в storage → getRefreshToken возвращает null (defensive)', () => {
    // Имитация ручного вмешательства/изменения схемы.
    window.sessionStorage.setItem(__REFRESH_STORAGE_KEY, '@@@broken-base64@@@');
    expect(getRefreshToken()).toBeNull();
  });

  it('перезапись токена: новый setRefreshToken перекрывает прежний', () => {
    setRefreshToken('first');
    setRefreshToken('second');
    expect(getRefreshToken()).toBe('second');
  });
});
