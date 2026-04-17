// Tab-resume handler (§5.7: «Tab Sleep → expired → на resume — silent refresh»).
//
// При переходе вкладки в `visible` проверяем: истёк ли access-токен
// (или осталось меньше REFRESH_LEAD_MS). Если да — триггерим doRefresh. Это
// защищает от случая «ноутбук спал 20 минут, таймер setTimeout не гарантирует
// тик в background-tabs большинства браузеров».
//
// Не трогает случай unauthenticated (нет tokenExpiry) — просто игнорит.
import { sessionStore } from '@/shared/auth/session-store';

import { doRefresh } from './actions';
import { REFRESH_LEAD_MS } from './constants';

let registered = false;

function onVisibilityChange(): void {
  if (typeof document === 'undefined') return;
  if (document.visibilityState !== 'visible') return;

  const { tokenExpiry } = sessionStore.getState();
  if (tokenExpiry === null) return;

  if (tokenExpiry - Date.now() <= REFRESH_LEAD_MS) {
    void doRefresh().catch(() => {
      // softLogout обработан внутри doRefresh.
    });
  }
}

/** Подписывается на `visibilitychange`. Идемпотентна. */
export function registerTabResume(): void {
  if (registered) return;
  if (typeof document === 'undefined') return;
  document.addEventListener('visibilitychange', onVisibilityChange);
  registered = true;
}

export function unregisterTabResume(): void {
  if (!registered) return;
  if (typeof document === 'undefined') return;
  document.removeEventListener('visibilitychange', onVisibilityChange);
  registered = false;
}
