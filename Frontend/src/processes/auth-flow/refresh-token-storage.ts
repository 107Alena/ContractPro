// Refresh-token storage (§5.2 таблица, ADR-FE-03 fallback).
//
// V1-режим: sessionStorage с обфускацией. НЕ защищает от XSS (любой код в
// странице читает sessionStorage), но усложняет casual-inspection в DevTools.
// Цель — уменьшить blast radius «подглядел соседний таб в open DevTools»,
// а не криптографическая защита. Миграция на HttpOnly;Secure cookie — §18 п.1.
//
// sessionStorage выбран над localStorage сознательно: refresh-токен живёт
// только в текущей вкладке, не пересекает вкладки и очищается при закрытии —
// минимизирует окно атаки.

const STORAGE_KEY = 'cp.rt.v1';
// XOR-ключ фиксированный, деривация из user-agent/браузерного fingerprint
// лишь создала бы иллюзию безопасности. Обфускация — чтобы токен не смотрелся
// как plain JWT при первом взгляде в DevTools.
const OBFUSCATION_KEY = 'contractpro-refresh-v1';

/** Available в тестах (node) и в браузере. SSR — noop. */
function getStorage(): Storage | null {
  if (typeof window === 'undefined') return null;
  try {
    return window.sessionStorage;
  } catch {
    // Privacy-mode Safari может бросать SecurityError при доступе к sessionStorage.
    return null;
  }
}

function xor(input: string, key: string): string {
  let out = '';
  for (let i = 0; i < input.length; i += 1) {
    const k = key.charCodeAt(i % key.length);
    out += String.fromCharCode(input.charCodeAt(i) ^ k);
  }
  return out;
}

function encode(token: string): string {
  // btoa принимает только Latin-1. JWT — ASCII base64url, поэтому OK без UTF-8 escape.
  return btoa(xor(token, OBFUSCATION_KEY));
}

function decode(payload: string): string | null {
  try {
    return xor(atob(payload), OBFUSCATION_KEY);
  } catch {
    // Повреждённые данные (изменён ключ, руками подправлено) — читаем как отсутствие.
    return null;
  }
}

export function getRefreshToken(): string | null {
  const storage = getStorage();
  if (!storage) return null;
  const raw = storage.getItem(STORAGE_KEY);
  if (!raw) return null;
  return decode(raw);
}

export function setRefreshToken(token: string): void {
  const storage = getStorage();
  if (!storage) return;
  storage.setItem(STORAGE_KEY, encode(token));
}

export function clearRefreshToken(): void {
  const storage = getStorage();
  if (!storage) return;
  storage.removeItem(STORAGE_KEY);
}

/** @internal — для тестов. */
export const __REFRESH_STORAGE_KEY = STORAGE_KEY;

/**
 * @internal — только для тестовой инфраструктуры (tests/e2e/fixtures/auth-state.ts,
 * FE-TASK-055). Возвращает то же значение, что записалось бы через `setRefreshToken`,
 * и позволяет Playwright-фикстуре сидировать sessionStorage без дублирования
 * XOR-ключа. Не использовать в продакшен-коде: обфускация — не безопасность.
 */
export function __encodeRefreshTokenForTests(token: string): string {
  return encode(token);
}
