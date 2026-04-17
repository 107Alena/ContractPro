// Auth-flow bootstrap (§5.1, §5.3, §5.7).
//
// initAuthFlow() вызывается в main.tsx ДО createRoot():
//  1. Регистрирует doRefresh как refreshHandler в axios-client.
//  2. Запускает silent-refresh timer.
//  3. Подписывается на visibilitychange для tab-resume.
//
// Почему до createRoot: React Router data-loaders запускаются синхронно при
// первом mount'е. Если access истёк и первый loader получит 401, interceptor
// должен уже иметь зарегистрированный handler (иначе → неконтролируемый reject
// вместо refresh + retry).
//
// setNavigator() регистрируется отдельно — там, где есть useNavigate (обычно
// внутри RouterProvider). До регистрации soft-logout редиректит через
// window.location.assign (с console.warn). Это degrade-safe fallback: хуже UX,
// но сессия всё равно завершится.
import { setRefreshHandler } from '@/shared/api/client';

import { doRefresh, setNavigator } from './actions';
import { registerTabResume, unregisterTabResume } from './tab-resume';
import { startSilentRefreshTimer, stopSilentRefreshTimer } from './timer';

let initialized = false;

export function initAuthFlow(): void {
  if (initialized) return;
  setRefreshHandler(doRefresh);
  startSilentRefreshTimer();
  registerTabResume();
  initialized = true;
}

/** Используется в тестах и на HMR-reload'е: полный teardown auth-flow. */
export function teardownAuthFlow(): void {
  if (!initialized) return;
  setRefreshHandler(null);
  setNavigator(null);
  stopSilentRefreshTimer();
  unregisterTabResume();
  initialized = false;
}

export { setNavigator };
