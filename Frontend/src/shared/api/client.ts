// HTTP-клиент Orchestrator-API (§7.2-7.4 high-architecture).
//
// Интерсепторы:
//   request:  inject Authorization: Bearer {access}, inject X-Correlation-Id (UUID v4).
//   response: 401 AUTH_TOKEN_EXPIRED → shared-promise refresh (§5.4) + replay;
//             429 → sleep(Retry-After) + 1 replay;
//             502/503 GET → 3 попытки exponential backoff (1s/2s/4s) (§7.4);
//             network error → 1 retry через 1с, затем OrchestratorError(NETWORK_ERROR);
//             прочие → OrchestratorError с полями из ErrorResponse.
//
// Цикл FE-TASK-012 ↔ FE-TASK-027 разорван DI-паттерном `setRefreshHandler`:
// client.ts не знает про auth-flow; FE-TASK-027 в init-hook регистрирует
// `doRefresh` (вызывает POST /auth/refresh через этот же http-инстанс).
//
// Тесты полагаются на MSW (node adapter). Для изоляции module-level state
// между тестами — `__resetForTests()` (exported for tests only).
import axios, {
  type AxiosError,
  type AxiosInstance,
  type AxiosResponse,
  type InternalAxiosRequestConfig,
} from 'axios';

import { sessionStore } from '@/shared/auth/session-store';

import { CLIENT_ERROR_CODES, type ErrorResponse, OrchestratorError } from './errors';

const DEFAULT_TIMEOUT_MS = 30_000;
const DEFAULT_RETRY_AFTER_SECONDS = 5;
const MAX_RETRY_AFTER_MS = 60_000;
const NETWORK_RETRY_DELAY_MS = 1_000;
const MAX_5XX_RETRIES = 3;
const BACKOFF_STEPS_MS = [1_000, 2_000, 4_000] as const;

export type RefreshHandler = () => Promise<string>;

// Module-level state. Разрыв цикла 012↔027 + shared-promise (§5.4) для refresh.
let refreshHandler: RefreshHandler | null = null;
let refreshInFlight: Promise<string> | null = null;

/**
 * Регистрирует функцию обновления токена (реализуется в FE-TASK-027 auth-flow).
 * Вызвать ДО первого HTTP-запроса, иначе 401 AUTH_TOKEN_EXPIRED будет
 * проброшен как OrchestratorError без попытки refresh.
 */
export function setRefreshHandler(fn: RefreshHandler | null): void {
  refreshHandler = fn;
}

// Internal: shared-promise обёртка вокруг refreshHandler. Гарантирует единственный
// in-flight refresh даже при N параллельных 401 (§5.4).
function getFreshToken(): Promise<string> {
  if (!refreshHandler) {
    return Promise.reject(
      new OrchestratorError({
        error_code: 'AUTH_TOKEN_EXPIRED',
        message: 'Сессия истекла. Войдите заново.',
        status: 401,
      }),
    );
  }
  if (refreshInFlight) return refreshInFlight;
  const handler = refreshHandler;
  refreshInFlight = handler().finally(() => {
    refreshInFlight = null;
  });
  return refreshInFlight;
}

// Кастомные флаги на конфиге запроса для идемпотентного retry-counting.
// Расширяем InternalAxiosRequestConfig через declaration merging, а не any.
declare module 'axios' {
  interface InternalAxiosRequestConfig {
    __retryAuth?: boolean;
    __retry429?: boolean;
    __retry5xxCount?: number;
    __retryNetwork?: boolean;
  }
}

/** AbortSignal-aware sleep. Rejects with DOMException('AbortError') если signal cancelled во время ожидания. */
function sleep(ms: number, signal?: AbortSignal | null): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException('Aborted', 'AbortError'));
      return;
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    const onAbort = (): void => {
      clearTimeout(timer);
      reject(new DOMException('Aborted', 'AbortError'));
    };
    signal?.addEventListener('abort', onAbort, { once: true });
  });
}

/**
 * Парсит HTTP-header Retry-After (RFC 7231): integer секунд ИЛИ HTTP-date.
 * Возвращает ms ≥ 0. Fallback — DEFAULT_RETRY_AFTER_SECONDS при пустом/невалидном
 * заголовке. Clamp верхней границей MAX_RETRY_AFTER_MS, чтобы предотвратить
 * «вечный» wait при сломанном сервере.
 */
export function parseRetryAfter(header: string | null | undefined): number {
  if (!header) return DEFAULT_RETRY_AFTER_SECONDS * 1000;
  const asNumber = Number(header);
  if (Number.isFinite(asNumber) && asNumber >= 0) {
    return Math.min(asNumber * 1000, MAX_RETRY_AFTER_MS);
  }
  const asDate = Date.parse(header);
  if (Number.isFinite(asDate)) {
    return Math.max(0, Math.min(asDate - Date.now(), MAX_RETRY_AFTER_MS));
  }
  return DEFAULT_RETRY_AFTER_SECONDS * 1000;
}

function generateCorrelationId(): string {
  // crypto.randomUUID доступен в браузерах (Safari 15.4+) и Node 19+ (наш baseline).
  // Fallback — math.random UUID v4-style на случай старых окружений (Jest w/o jsdom).
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  const rand = (): string =>
    Math.floor(Math.random() * 0x10000)
      .toString(16)
      .padStart(4, '0');
  return `${rand()}${rand()}-${rand()}-4${rand().slice(1)}-${rand()}-${rand()}${rand()}${rand()}`;
}

function isErrorResponseBody(body: unknown): body is ErrorResponse {
  return (
    typeof body === 'object' &&
    body !== null &&
    'error_code' in body &&
    typeof (body as { error_code: unknown }).error_code === 'string' &&
    'message' in body &&
    typeof (body as { message: unknown }).message === 'string'
  );
}

function toOrchestratorError(err: AxiosError<ErrorResponse>): OrchestratorError {
  const status = err.response?.status;
  const body = err.response?.data;
  const headerId = err.response?.headers?.['x-correlation-id'];
  const correlationId =
    (isErrorResponseBody(body) && body.correlation_id) ||
    (typeof headerId === 'string' ? headerId : undefined);

  // Не прокидываем AxiosError как `cause`: его `config` содержит функции
  // (transformRequest/transformResponse), которые structuredClone не сериализует —
  // Vitest worker падает с DataCloneError при постинге unhandled rejection.
  if (isErrorResponseBody(body)) {
    return new OrchestratorError({
      error_code: body.error_code,
      message: body.message,
      ...(body.suggestion !== undefined && { suggestion: body.suggestion }),
      ...(body.details !== undefined && { details: body.details }),
      ...(correlationId !== undefined && { correlationId }),
      ...(status !== undefined && { status }),
    });
  }

  // Нет распознаваемого ErrorResponse-тела — классифицируем по AxiosError.code / отсутствию response.
  const code =
    err.code === 'ECONNABORTED' || err.code === 'ETIMEDOUT'
      ? CLIENT_ERROR_CODES.TIMEOUT
      : err.code === 'ERR_CANCELED'
        ? CLIENT_ERROR_CODES.REQUEST_ABORTED
        : // Нет response и не abort/timeout → трактуем как network error.
          // MSW v2 node adapter выдаёт err.code === 'ERR_NETWORK' непоследовательно
          // в зависимости от undici-версии; полагаемся на факт отсутствия response.
          !err.response && err.code !== 'ERR_BAD_RESPONSE'
          ? CLIENT_ERROR_CODES.NETWORK_ERROR
          : CLIENT_ERROR_CODES.UNKNOWN_ERROR;
  const message =
    code === CLIENT_ERROR_CODES.NETWORK_ERROR
      ? 'Нет соединения с сервером. Проверьте подключение.'
      : code === CLIENT_ERROR_CODES.TIMEOUT
        ? 'Превышено время ожидания ответа сервера.'
        : code === CLIENT_ERROR_CODES.REQUEST_ABORTED
          ? 'Запрос был отменён.'
          : 'Произошла ошибка. Мы уже знаем.';
  return new OrchestratorError({
    error_code: code,
    message,
    ...(correlationId !== undefined && { correlationId }),
    ...(status !== undefined && { status }),
  });
}

export function createHttpClient(baseURL = '/api/v1'): AxiosInstance {
  const instance = axios.create({
    baseURL,
    timeout: DEFAULT_TIMEOUT_MS,
    // withCredentials: false (v1 — refresh-token в sessionStorage, ADR-FE-03).
    // При миграции на HttpOnly cookie (§18 backlog) переключить на true.
  });

  instance.interceptors.request.use((cfg: InternalAxiosRequestConfig) => {
    const token = sessionStore.getState().accessToken;
    if (token && !cfg.headers.has('Authorization')) {
      cfg.headers.set('Authorization', `Bearer ${token}`);
    }
    if (!cfg.headers.has('X-Correlation-Id')) {
      cfg.headers.set('X-Correlation-Id', generateCorrelationId());
    }
    return cfg;
  });

  instance.interceptors.response.use(
    (res: AxiosResponse) => res,
    async (err: AxiosError<ErrorResponse>) => {
      const config = err.config;
      if (!config) throw toOrchestratorError(err);

      const signal = config.signal as AbortSignal | undefined;
      if (signal?.aborted) throw toOrchestratorError(err);

      const status = err.response?.status;
      const body = err.response?.data;

      // 1. 401 AUTH_TOKEN_EXPIRED → shared-promise refresh (§5.4) + replay.
      // __retryAuth-guard предотвращает бесконечную петлю, если refresh прошёл,
      // но запрос снова получил 401 (revoked access / refresh-token invalidated).
      if (
        status === 401 &&
        isErrorResponseBody(body) &&
        body.error_code === 'AUTH_TOKEN_EXPIRED' &&
        !config.__retryAuth
      ) {
        config.__retryAuth = true;
        try {
          await getFreshToken();
          // request interceptor вновь пропишет Authorization — снимаем старый
          // заголовок, чтобы гарантированно подхватить свежий токен.
          config.headers.delete('Authorization');
          return instance.request(config);
        } catch (refreshErr) {
          if (refreshErr instanceof OrchestratorError) throw refreshErr;
          throw toOrchestratorError(err);
        }
      }

      // 2. 429 → ждём Retry-After, повторяем 1 раз (§7.4).
      if (status === 429 && !config.__retry429) {
        config.__retry429 = true;
        // axios headers — string | string[] | number | boolean | undefined.
        // typeof-guard для консистентности с x-correlation-id narrow'ом ниже.
        const rawRA = err.response?.headers?.['retry-after'];
        const retryAfterHeader = typeof rawRA === 'string' ? rawRA : undefined;
        const waitMs = parseRetryAfter(retryAfterHeader);
        await sleep(waitMs, signal);
        return instance.request(config);
      }

      // 3. 502/503 → 3 попытки exponential backoff (1s/2s/4s). Только идемпотентный GET.
      if ((status === 502 || status === 503) && (config.method ?? 'get').toLowerCase() === 'get') {
        const attempt = config.__retry5xxCount ?? 0;
        if (attempt < MAX_5XX_RETRIES) {
          config.__retry5xxCount = attempt + 1;
          await sleep(
            BACKOFF_STEPS_MS[attempt] ?? BACKOFF_STEPS_MS[BACKOFF_STEPS_MS.length - 1]!,
            signal,
          );
          return instance.request(config);
        }
      }

      // 4. Network error (нет err.response и не abort/timeout): 1 retry через 1с (§7.4).
      // Проверяем факт отсутствия response, а не err.code — MSW node adapter /
      // undici выставляют `code` непоследовательно.
      if (
        !err.response &&
        err.code !== 'ECONNABORTED' &&
        err.code !== 'ETIMEDOUT' &&
        err.code !== 'ERR_CANCELED' &&
        !config.__retryNetwork
      ) {
        config.__retryNetwork = true;
        await sleep(NETWORK_RETRY_DELAY_MS, signal);
        return instance.request(config);
      }

      // 5. Нормализация доменной ошибки.
      throw toOrchestratorError(err);
    },
  );

  return instance;
}

export const http: AxiosInstance = createHttpClient();

/**
 * Сбросить module-level state. Используется только в тестах между кейсами.
 * @internal
 */
export function __resetForTests(): void {
  refreshHandler = null;
  refreshInFlight = null;
}
