import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { User } from './session-store';
import { sessionStore, useSession } from './session-store';

const makeUser = (overrides: Partial<User> = {}): User => ({
  user_id: '00000000-0000-0000-0000-000000000001',
  email: 'lawyer@example.com',
  name: 'Иван Иванов',
  role: 'LAWYER',
  organization_id: '00000000-0000-0000-0000-000000000aa1',
  organization_name: 'ООО «Контракт»',
  permissions: { export_enabled: true },
  ...overrides,
});

describe('session-store', () => {
  beforeEach(() => {
    useSession.getState().clear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('имеет пустое начальное состояние', () => {
    const s = useSession.getState();
    expect(s.accessToken).toBeNull();
    expect(s.user).toBeNull();
    expect(s.tokenExpiry).toBeNull();
  });

  it('setAccess сохраняет токен и абсолютный epoch-ms истечения (§5.3)', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-04-17T12:00:00Z'));

    useSession.getState().setAccess('jwt-abc', 900); // 15 мин = 900 s

    const s = useSession.getState();
    expect(s.accessToken).toBe('jwt-abc');
    expect(s.tokenExpiry).toBe(new Date('2026-04-17T12:15:00Z').getTime());
  });

  it('setUser сохраняет профиль с role и permissions', () => {
    useSession.getState().setUser(makeUser({ role: 'ORG_ADMIN' }));

    expect(useSession.getState().user?.role).toBe('ORG_ADMIN');
    expect(useSession.getState().user?.permissions.export_enabled).toBe(true);
  });

  it('setAccess и setUser независимы: обновление одного не сбрасывает другое', () => {
    useSession.getState().setAccess('token-1', 60);
    useSession.getState().setUser(makeUser());
    useSession.getState().setAccess('token-2', 120);

    const s = useSession.getState();
    expect(s.accessToken).toBe('token-2');
    expect(s.user?.email).toBe('lawyer@example.com');
  });

  it('clear() обнуляет accessToken, user и tokenExpiry', () => {
    useSession.getState().setAccess('t', 60);
    useSession.getState().setUser(makeUser({ role: 'BUSINESS_USER' }));

    useSession.getState().clear();

    const s = useSession.getState();
    expect(s.accessToken).toBeNull();
    expect(s.user).toBeNull();
    expect(s.tokenExpiry).toBeNull();
  });

  it('sessionStore — алиас того же store (ADR-FE-03, §7.2 axios-интерсептор)', () => {
    useSession.getState().setAccess('shared-token', 60);
    expect(sessionStore.getState().accessToken).toBe('shared-token');
  });

  it('subscribe нотифицирует non-React потребителей об изменениях (§5.3 refresh-таймер)', () => {
    const listener = vi.fn();
    const unsub = sessionStore.subscribe(listener);

    useSession.getState().setAccess('t', 60);
    useSession.getState().setUser(makeUser());
    useSession.getState().clear();

    expect(listener).toHaveBeenCalledTimes(3);
    unsub();
  });

  it('селектор role возвращает undefined без user', () => {
    const selectRole = (s: ReturnType<typeof useSession.getState>): User['role'] | undefined =>
      s.user?.role;
    expect(selectRole(useSession.getState())).toBeUndefined();

    useSession.getState().setUser(makeUser({ role: 'BUSINESS_USER' }));
    expect(selectRole(useSession.getState())).toBe('BUSINESS_USER');
  });

  it('селектор permissions.export_enabled поддерживает вычисление useCanExport (§5.6)', () => {
    const selectExport = (s: ReturnType<typeof useSession.getState>): boolean | undefined =>
      s.user?.permissions?.export_enabled;

    useSession
      .getState()
      .setUser(makeUser({ role: 'BUSINESS_USER', permissions: { export_enabled: false } }));
    expect(selectExport(useSession.getState())).toBe(false);

    useSession
      .getState()
      .setUser(makeUser({ role: 'BUSINESS_USER', permissions: { export_enabled: true } }));
    expect(selectExport(useSession.getState())).toBe(true);
  });

  it('setAccess с нулевым expiresIn создаёт истёкший токен (для edge-case refresh flow)', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-04-17T12:00:00Z'));

    useSession.getState().setAccess('expired', 0);

    expect(useSession.getState().tokenExpiry).toBe(Date.now());
  });
});
