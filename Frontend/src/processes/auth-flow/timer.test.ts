// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { sessionStore } from '@/shared/auth/session-store';

// vi.mock hoisted: factory не видит переменные из верхней части файла.
// Обходится vi.hoisted() — он гарантирует, что mock-factory и тесты делят
// одну и ту же ссылку на doRefreshMock.
const { doRefreshMock } = vi.hoisted(() => ({
  doRefreshMock: vi.fn(async () => 'new-access'),
}));
vi.mock('./actions', () => ({
  doRefresh: doRefreshMock,
}));

import {
  __hasPendingTimerForTests,
  startSilentRefreshTimer,
  stopSilentRefreshTimer,
} from './timer';

describe('silent-refresh timer (§5.3)', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    doRefreshMock.mockClear();
    // Явный системный момент для предсказуемых delay-расчётов.
    vi.setSystemTime(new Date('2026-04-17T10:00:00.000Z'));
  });

  afterEach(() => {
    stopSilentRefreshTimer();
    sessionStore.getState().clear();
    vi.useRealTimers();
  });

  it('startSilentRefreshTimer без access-токена → таймер не запланирован', () => {
    startSilentRefreshTimer();
    expect(__hasPendingTimerForTests()).toBe(false);
    expect(doRefreshMock).not.toHaveBeenCalled();
  });

  it('setAccess(expiresIn=900) → таймер срабатывает за 60с до exp (840s)', async () => {
    startSilentRefreshTimer();
    sessionStore.getState().setAccess('tok', 900);

    expect(__hasPendingTimerForTests()).toBe(true);
    // 900s exp - 60s lead = 840s до triggerRefresh.
    await vi.advanceTimersByTimeAsync(839_000);
    expect(doRefreshMock).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(1_000);
    expect(doRefreshMock).toHaveBeenCalledTimes(1);
  });

  it('setAccess с expiresIn < 60s → immediate refresh (без setTimeout)', async () => {
    startSilentRefreshTimer();
    sessionStore.getState().setAccess('tok', 30);
    // scheduleFor считает delay = tokenExpiry - 60_000 - now, что < 0 → immediate call.
    // Промисы внутри triggerRefresh нужно дождаться:
    await vi.advanceTimersByTimeAsync(0);
    expect(doRefreshMock).toHaveBeenCalledTimes(1);
  });

  it('повторный setAccess → отменяет старый таймер и планирует новый', async () => {
    startSilentRefreshTimer();
    sessionStore.getState().setAccess('tok-1', 900);
    expect(__hasPendingTimerForTests()).toBe(true);

    // До истечения первого таймера получаем новый токен (как после doRefresh).
    await vi.advanceTimersByTimeAsync(100_000);
    sessionStore.getState().setAccess('tok-2', 900);

    // Первый таймер был бы за 840s от момента 0; теперь нужен новый таймер
    // от текущего момента (100s) на ещё 840s = абсолют 940s.
    await vi.advanceTimersByTimeAsync(839_000); // в сумме 939s
    expect(doRefreshMock).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(2_000); // 941s
    expect(doRefreshMock).toHaveBeenCalledTimes(1);
  });

  it('clear() → отменяет таймер', async () => {
    startSilentRefreshTimer();
    sessionStore.getState().setAccess('tok', 900);
    expect(__hasPendingTimerForTests()).toBe(true);

    sessionStore.getState().clear();
    expect(__hasPendingTimerForTests()).toBe(false);

    await vi.advanceTimersByTimeAsync(1_000_000);
    expect(doRefreshMock).not.toHaveBeenCalled();
  });

  it('stopSilentRefreshTimer отменяет таймер и отписывается', async () => {
    startSilentRefreshTimer();
    sessionStore.getState().setAccess('tok', 900);

    stopSilentRefreshTimer();
    expect(__hasPendingTimerForTests()).toBe(false);

    // После stop — новые setAccess не планируют таймер.
    sessionStore.getState().setAccess('tok-2', 900);
    expect(__hasPendingTimerForTests()).toBe(false);

    await vi.advanceTimersByTimeAsync(1_000_000);
    expect(doRefreshMock).not.toHaveBeenCalled();
  });

  it('идемпотентность: двойной startSilentRefreshTimer не даёт дубль-подписки', async () => {
    startSilentRefreshTimer();
    startSilentRefreshTimer();

    sessionStore.getState().setAccess('tok', 30);
    // expiresIn < 60s → immediate trigger. Одна подписка → один вызов.
    await vi.advanceTimersByTimeAsync(0);
    expect(doRefreshMock).toHaveBeenCalledTimes(1);
  });
});
