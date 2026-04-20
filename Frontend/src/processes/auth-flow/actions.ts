// Auth-flow actions: login / doRefresh / logout (§5.1, §5.4, §5.7 high-architecture).
//
// Архитектурные решения:
// * Модуль использует shared `http` из `@/shared/api` — тот же инстанс, что и
//   продакт-запросы. Иначе interceptor'ы (Authorization, X-Correlation-Id,
//   429/5xx-retry) не применились бы к auth-эндпоинтам.
// * `doRefresh` — отдельный named-export. Регистрируется как handler через
//   `setRefreshHandler(doRefresh)` в `setup.ts`; axios-client шарит его
//   результат через свой module-level `refreshInFlight` (§7.2), так что
//   параллельные 401 + silent-timer попадают в один in-flight запрос.
// * Silent-логаут (refresh failure / не знаем юзера) не выполняет POST /auth/logout —
//   refresh-token уже мёртв, дёргать UOM бессмысленно. Просто чистим всё клиентское.
// * `logout()` — best-effort POST /auth/logout: если сервер недоступен,
//   всё равно чистим клиентские токены и редиректим на /login.
//
// Цикл FE-TASK-012 ↔ FE-TASK-027 разорван: client.ts не импортит actions;
// actions импортит `http`. refreshHandler инжектируется через setRefreshHandler
// в setup.ts (запускается до первого запроса).
import type { AxiosInstance } from 'axios';

import { http, OrchestratorError, queryClient } from '@/shared/api';
import type { components } from '@/shared/api/openapi';
import { sessionStore } from '@/shared/auth/session-store';
import { emitRumEvent } from '@/shared/observability';
import { toast } from '@/shared/ui/toast';

import { clearRefreshToken, getRefreshToken, setRefreshToken } from './refresh-token-storage';

// Module-level http-инстанс. По умолчанию — shared `http` из `@/shared/api`.
// Тесты могут переопределить через `__setHttpForTests` на инстанс
// `createHttpClient(BASE)` для работы с MSW (jsdom + axios default adapter →
// XMLHttpRequest, который не перехватывает MSW node-adapter).
let httpInstance: AxiosInstance = http;

/** @internal — для тестов. */
export function __setHttpForTests(instance: AxiosInstance | null): void {
  httpInstance = instance ?? http;
}

type AuthTokens = components['schemas']['AuthTokens'];
type LoginRequest = components['schemas']['LoginRequest'];
type UserProfile = components['schemas']['UserProfile'];

export interface LoginCredentials {
  email: string;
  password: string;
}

/**
 * Callback для редиректа после soft-logout. Задаётся через `setNavigator` в
 * setup.ts (для интеграции c React Router) либо fallback — `window.location.assign`.
 */
type Navigator = (path: string) => void;
let navigateFn: Navigator | null = null;

export function setNavigator(fn: Navigator | null): void {
  navigateFn = fn;
}

function redirect(path: string): void {
  if (navigateFn) {
    navigateFn(path);
    return;
  }
  if (typeof window !== 'undefined') {
    console.warn('[auth-flow] setNavigator не вызван — редирект через window.location.assign');
    window.location.assign(path);
  }
}

const SESSION_EXPIRED_TOAST_ID = 'auth.session-expired';

/**
 * Полное очищение клиентской сессии: access-токен, refresh-токен, TanStack Query
 * cache. Не вызывает серверный /auth/logout (для happy-path logout() делает это
 * отдельно). Безопасно вызывать многократно — идемпотентно.
 */
function clearClientSession(): void {
  sessionStore.getState().clear();
  clearRefreshToken();
  queryClient.clear();
}

function assertTokens(
  tokens: AuthTokens,
): asserts tokens is Required<Pick<AuthTokens, 'access_token' | 'refresh_token' | 'expires_in'>> {
  if (!tokens.access_token || !tokens.refresh_token || typeof tokens.expires_in !== 'number') {
    throw new OrchestratorError({
      error_code: 'AUTH_INVALID_RESPONSE',
      message: 'Некорректный ответ сервера авторизации.',
    });
  }
}

function assertAccessTokens(
  tokens: AuthTokens,
): asserts tokens is Required<Pick<AuthTokens, 'access_token' | 'expires_in'>> & AuthTokens {
  if (!tokens.access_token || typeof tokens.expires_in !== 'number') {
    throw new OrchestratorError({
      error_code: 'AUTH_INVALID_RESPONSE',
      message: 'Некорректный ответ сервера авторизации.',
    });
  }
}

/**
 * POST /auth/login → сохраняет access в memory, refresh в sessionStorage,
 * затем GET /users/me → сохраняет UserProfile. Возвращает UserProfile.
 * Throws OrchestratorError при 401/VALIDATION_ERROR/etc.
 */
export async function login(credentials: LoginCredentials): Promise<UserProfile> {
  const loginBody: LoginRequest = { email: credentials.email, password: credentials.password };
  const { data: tokens } = await httpInstance.post<AuthTokens>('/auth/login', loginBody);
  assertTokens(tokens);

  sessionStore.getState().setAccess(tokens.access_token, tokens.expires_in);
  setRefreshToken(tokens.refresh_token);

  const { data: user } = await httpInstance.get<UserProfile>('/users/me');
  sessionStore.getState().setUser(user);
  return user;
}

/**
 * POST /auth/refresh → новый access-токен. Идемпотентность обеспечивается
 * `refreshInFlight` в `client.ts` (§5.4 shared-promise). При отсутствии
 * refresh-токена или 401 — инициируется soft-logout и функция rejects
 * с OrchestratorError (axios-interceptor прокидывает ошибку в исходный запрос).
 *
 * Ошибка `AUTH_REFRESH_FAILED` сигнализирует вызывающему (interceptor / timer)
 * что текущая сессия мертва.
 */
export async function doRefresh(): Promise<string> {
  const refreshToken = getRefreshToken();
  if (!refreshToken) {
    // RUM: auth.refresh.failed (§14.4) — причина "no_token" (пользователь
    // зашёл без cookie / sessionStorage пуст).
    emitRumEvent('auth.refresh.failed', { reason: 'no_token' });
    softLogout();
    throw new OrchestratorError({
      error_code: 'AUTH_REFRESH_FAILED',
      message: 'Сессия завершена. Войдите заново.',
      status: 401,
    });
  }

  try {
    const { data: tokens } = await httpInstance.post<AuthTokens>('/auth/refresh', {
      refresh_token: refreshToken,
    });
    assertAccessTokens(tokens);
    sessionStore.getState().setAccess(tokens.access_token, tokens.expires_in);
    // Rotate: сервер может вернуть новый refresh-токен (AuthTokens содержит оба поля).
    // Если не вернул — оставляем прежний.
    if (tokens.refresh_token) setRefreshToken(tokens.refresh_token);
    return tokens.access_token;
  } catch (err) {
    // RUM: auth.refresh.failed (§14.4). reason — error_code из OrchestratorError
    // (обычно AUTH_REFRESH_FAILED / AUTH_TOKEN_EXPIRED) либо "network".
    const reason = err instanceof OrchestratorError ? err.error_code : 'network';
    emitRumEvent('auth.refresh.failed', { reason });
    softLogout();
    if (err instanceof OrchestratorError) throw err;
    throw new OrchestratorError({
      error_code: 'AUTH_REFRESH_FAILED',
      message: 'Сессия завершена. Войдите заново.',
      status: 401,
    });
  }
}

/**
 * Soft-logout: без серверного /auth/logout (токен уже невалиден либо отсутствует).
 * Показывает sticky-toast «Сессия завершена», чистит клиентское состояние,
 * редиректит на /login. Используется refresh-failure flow и tab-resume при
 * 401 на retry.
 */
export function softLogout(): void {
  clearClientSession();
  toast.sticky({
    id: SESSION_EXPIRED_TOAST_ID,
    title: 'Сессия завершена',
    description: 'Пожалуйста, войдите снова.',
  });
  redirect('/login');
}

/**
 * Happy-path logout: отправляет POST /auth/logout с текущим refresh-токеном
 * (best-effort — ошибки сервера не блокируют редирект), затем чистит клиент и
 * редиректит. Не показывает sticky-toast — это явное пользовательское действие.
 */
export async function logout(): Promise<void> {
  const refreshToken = getRefreshToken();
  if (refreshToken) {
    try {
      await httpInstance.post('/auth/logout', { refresh_token: refreshToken });
    } catch {
      // best-effort — сервер мог быть недоступен, но логаут должен пройти на клиенте.
    }
  }
  clearClientSession();
  redirect('/login');
}

/** @internal — для тестов: сброс модульного navigator'а. */
export function __resetNavigatorForTests(): void {
  navigateFn = null;
}
