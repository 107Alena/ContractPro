// OpenTelemetry browser SDK init (§14.3 high-architecture, FE-TASK-051).
//
// Ingress: `window.__ENV__.OTEL_ENDPOINT` (runtime-config, §13.5) — при пустом
// endpoint init — no-op (локальный dev без collector'а).
//
// Instrumentation: `instrumentation-fetch` + `instrumentation-xml-http-request`.
// Axios в браузере использует XHR adapter → без XHR-инструментации span'ы от
// API-вызовов теряются. Обе инструментации получают единый enrichment-callback.
//
// Distributed tracing:
//   - `traceparent` инжектится автоматически (§14.3). Same-origin deployment
//     (ADR-6 backend) исключает CORS-блок — `propagateTraceHeaderCorsUrls`
//     оставляем default, чтобы не утекал traceparent в cross-origin при
//     непреднамеренном вызове внешнего хоста.
//   - Sentry Performance отключён (`tracesSampleRate:0` в `sentry.ts`) —
//     единая точка сбора trace'ов — OpenTelemetry. См. §14.3 + комментарий
//     в `sentry.ts:37-40`.
//
// Ограничение: EventSource API не инструментируется (ни fetch, ни XHR его не
// покрывают). SSE-стрим `/api/v1/events/stream` (§7.7) не получает
// auto-injection `traceparent`; корреляция через `correlation_id` в payload
// (поле в StatusEvent, §4.4). Full distributed trace через OTel не покрыт.
import { type Span, trace } from '@opentelemetry/api';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { registerInstrumentations } from '@opentelemetry/instrumentation';
import { FetchInstrumentation } from '@opentelemetry/instrumentation-fetch';
import { XMLHttpRequestInstrumentation } from '@opentelemetry/instrumentation-xml-http-request';
import { Resource } from '@opentelemetry/resources';
import { BatchSpanProcessor, WebTracerProvider } from '@opentelemetry/sdk-trace-web';
import { ATTR_SERVICE_NAME, ATTR_SERVICE_VERSION } from '@opentelemetry/semantic-conventions';

import { sessionStore } from '@/shared/auth/session-store';
import { getRuntimeEnv, type RuntimeEnv } from '@/shared/config';

const SERVICE_NAME = 'contractpro-frontend';
const DEPLOYMENT_ENVIRONMENT_ATTR = 'deployment.environment';

export interface InitOtelResult {
  enabled: boolean;
}

export interface OtelProviderDescriptor {
  endpoint: string;
  resourceAttributes: Record<string, string>;
}

/**
 * Git SHA, инжектируемый на build-time через Vite `define __GIT_SHA__`
 * (см. `vite.config.ts`). Пустая строка — fallback, если VITE_GIT_SHA не
 * задан и git-контекст недоступен (Docker build без .git).
 */
declare const __GIT_SHA__: string;

/**
 * Чистый билдер OTel-конфига — выделен в отдельную функцию (паттерн
 * `buildSentryConfig`) для тестирования без регистрации глобального
 * trace-API. Возвращает null, если OTEL_ENDPOINT отсутствует.
 */
export function buildOtelConfig(
  env: RuntimeEnv,
  gitSha: string,
  mode: string,
): OtelProviderDescriptor | null {
  const endpoint = env.OTEL_ENDPOINT;
  if (!endpoint) return null;

  const resourceAttributes: Record<string, string> = {
    [ATTR_SERVICE_NAME]: SERVICE_NAME,
    [DEPLOYMENT_ENVIRONMENT_ATTR]: mode,
  };
  if (gitSha) {
    resourceAttributes[ATTR_SERVICE_VERSION] = gitSha;
  }

  return { endpoint, resourceAttributes };
}

type RequestLike = Request | RequestInit | XMLHttpRequest;

// XHR correlation-id store: `applyCustomAttributesOnSpan` получает XHR, но
// XHR API не раскрывает ранее установленные request-headers читающему
// (setRequestHeader — write-only). Поэтому axios-interceptor должен явно
// сообщить correlation_id через `tagXhrCorrelationId(xhr, id)` — см.
// export ниже и TODO(FE-TASK-052) в client.ts interceptor'е.
//
// WeakMap (не property на XHR) сохраняет grep'абельность контракта и не
// мусорит полями на объекте, видимыми DevTools / сторонним кодом.
const xhrCorrelationIds = new WeakMap<XMLHttpRequest, string>();

/**
 * Связать correlation_id с конкретным XHR-инстансом, чтобы enrichSpan мог
 * прочитать его при финализации span'а. Вызывается axios-interceptor'ом
 * ДО xhr.send() (§7.2).
 *
 * TODO(FE-TASK-052): добавить вызов из axios.adapter-wrapper, чтобы для
 * XHR-span'ов `app.correlation_id` действительно заполнялся. Сейчас v1:
 * для XHR attr остаётся undefined (accepted limitation); для Fetch — работает.
 */
export function tagXhrCorrelationId(xhr: XMLHttpRequest, id: string): void {
  xhrCorrelationIds.set(xhr, id);
}

function readCorrelationId(request: RequestLike): string | undefined {
  if (typeof Request !== 'undefined' && request instanceof Request) {
    return request.headers.get('X-Correlation-Id') ?? undefined;
  }
  if (typeof XMLHttpRequest !== 'undefined' && request instanceof XMLHttpRequest) {
    return xhrCorrelationIds.get(request);
  }
  const headers = (request as RequestInit).headers;
  if (!headers) return undefined;
  if (typeof Headers !== 'undefined' && headers instanceof Headers) {
    return headers.get('X-Correlation-Id') ?? undefined;
  }
  if (Array.isArray(headers)) {
    const entry = headers.find(([k]) => k.toLowerCase() === 'x-correlation-id');
    return entry?.[1];
  }
  const record = headers as Record<string, string>;
  return record['X-Correlation-Id'] ?? record['x-correlation-id'];
}

/**
 * Enrichment callback: добавляет app-атрибуты на HTTP-span'ы от fetch и XHR.
 * Читает `sessionStore` лениво (span-creation time), чтобы события,
 * испущенные до авторизации, имели актуальные значения после login.
 *
 * Атрибуты (§14.3):
 *   - `app.user_role`       — из sessionStore.user.role
 *   - `app.org_id`          — из sessionStore.user.organization_id
 *   - `app.correlation_id`  — из X-Correlation-Id (per-request UUID v4)
 *   - `app.http_path`       — URL.pathname (не `http.route` — semconv требует
 *                              templated route `/contracts/:id`, которого на
 *                              клиенте нет без router-aware резолвера).
 */
export function enrichSpan(span: Span, request: RequestLike): void {
  const user = sessionStore.getState().user;
  if (user) {
    span.setAttribute('app.user_role', user.role);
    span.setAttribute('app.org_id', user.organization_id);
  }
  const correlationId = readCorrelationId(request);
  if (correlationId) {
    span.setAttribute('app.correlation_id', correlationId);
  }
  const url = extractUrl(request);
  if (url) {
    span.setAttribute('app.http_path', url);
  }
}

function extractUrl(request: RequestLike): string | undefined {
  if (typeof Request !== 'undefined' && request instanceof Request) {
    try {
      return new URL(request.url).pathname;
    } catch {
      return undefined;
    }
  }
  if (typeof XMLHttpRequest !== 'undefined' && request instanceof XMLHttpRequest) {
    const tagged = request as XMLHttpRequest & { responseURL?: string };
    if (!tagged.responseURL) return undefined;
    try {
      return new URL(tagged.responseURL).pathname;
    } catch {
      return undefined;
    }
  }
  return undefined;
}

// Idempotency guard: защита от повторной регистрации при StrictMode-повторе
// или случайных двойных вызовах initOtel() из разных точек загрузки.
let initialized = false;

/**
 * Инициализирует OpenTelemetry браузер-SDK, если в runtime-env задан
 * OTEL_ENDPOINT. При пустом endpoint — no-op.
 */
export function initOtel(): InitOtelResult {
  if (initialized) return { enabled: true };
  const gitSha = typeof __GIT_SHA__ !== 'undefined' ? __GIT_SHA__ : '';
  const config = buildOtelConfig(getRuntimeEnv(), gitSha, import.meta.env.MODE);
  if (!config) return { enabled: false };

  const provider = new WebTracerProvider({
    resource: new Resource(config.resourceAttributes),
  });
  provider.addSpanProcessor(
    new BatchSpanProcessor(new OTLPTraceExporter({ url: config.endpoint })),
  );
  provider.register();

  registerInstrumentations({
    instrumentations: [
      new FetchInstrumentation({ applyCustomAttributesOnSpan: enrichSpan }),
      new XMLHttpRequestInstrumentation({ applyCustomAttributesOnSpan: enrichSpan }),
    ],
  });

  initialized = true;
  return { enabled: true };
}

/** Для тестов: сброс guard'а + отключение глобального tracer. */
export function __resetOtelForTests(): void {
  initialized = false;
  trace.disable();
}
