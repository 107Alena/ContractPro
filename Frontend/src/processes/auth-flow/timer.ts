// Silent-refresh timer (§5.3 high-architecture).
//
// Подписан на `sessionStore` (tokenExpiry). При каждом setAccess перепланирует
// setTimeout на `tokenExpiry - 60_000 - Date.now()`:
//  * будущее → ставим таймер, при тике вызываем doRefresh();
//  * уже прошло / осталось <60s → триггерим doRefresh() сразу;
//  * tokenExpiry === null (clear) → отменяем таймер.
//
// Таймер — тот же doRefresh, что и axios-interceptor. Второй уровень
// shared-promise не нужен: `refreshInFlight` в client.ts объединяет оба пути.
//
// Ошибки doRefresh НЕ эскалируются наверх (таймер работает в фоне). Они уже
// обработаны внутри doRefresh: при failure вызывается softLogout.
import { sessionStore } from '@/shared/auth/session-store';

import { doRefresh } from './actions';
import { REFRESH_LEAD_MS } from './constants';

type TimerId = ReturnType<typeof setTimeout>;
let timerId: TimerId | null = null;
let unsubscribe: (() => void) | null = null;

function scheduleFor(tokenExpiry: number | null): void {
  clearPendingTimer();
  if (tokenExpiry === null) return;

  const delay = tokenExpiry - REFRESH_LEAD_MS - Date.now();
  if (delay <= 0) {
    // Токен уже истёк или истекает в ближайшие 60с. Стартуем refresh сейчас.
    void triggerRefresh();
    return;
  }
  timerId = setTimeout(() => {
    timerId = null;
    void triggerRefresh();
  }, delay);
}

function clearPendingTimer(): void {
  if (timerId !== null) {
    clearTimeout(timerId);
    timerId = null;
  }
}

async function triggerRefresh(): Promise<void> {
  try {
    await doRefresh();
    // doRefresh вызывает setAccess → subscribe-обработчик перепланирует таймер
    // на новый exp. Никаких ручных reschedule здесь не требуется.
  } catch {
    // softLogout уже вызван внутри doRefresh; таймер остановлен через clearPendingTimer
    // (запланируем в startSilentRefreshTimer через вторую передачу tokenExpiry=null).
  }
}

/**
 * Запускает silent-refresh timer. Идемпотентна: повторные вызовы переписывают
 * подписку и переигрывают таймер. Для teardown см. `stopSilentRefreshTimer`.
 */
export function startSilentRefreshTimer(): void {
  stopSilentRefreshTimer();

  // Инициальный schedule — на случай, если accessToken уже выставлен до старта
  // таймера (actually рестарт сессии из persisted refresh).
  scheduleFor(sessionStore.getState().tokenExpiry);

  unsubscribe = sessionStore.subscribe((state, prev) => {
    if (state.tokenExpiry === prev.tokenExpiry) return;
    scheduleFor(state.tokenExpiry);
  });
}

/** Останавливает таймер и отписывается от store. Идемпотентна. */
export function stopSilentRefreshTimer(): void {
  clearPendingTimer();
  if (unsubscribe) {
    unsubscribe();
    unsubscribe = null;
  }
}

/** @internal — для тестов. Возвращает true если таймер сейчас запланирован. */
export function __hasPendingTimerForTests(): boolean {
  return timerId !== null;
}
