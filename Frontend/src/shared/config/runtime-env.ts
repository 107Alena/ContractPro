// Доступ к runtime-конфигурации, инжектируемой через `window.__ENV__`
// (см. §13.5 high-architecture: nginx отдаёт /config.js с no-store).
// До FE-TASK-030 (App shell) и FE-TASK-009 (Dockerfile + entrypoint.sh) объект
// может отсутствовать — guard возвращает пустой объект, FEATURES трактуется
// как «все фича-флаги выключены».
export type FeatureFlag = 'FEATURE_SSO' | 'FEATURE_DOCX_UPLOAD';

export type FeatureFlags = Partial<Record<FeatureFlag, boolean>>;

export interface RuntimeEnv {
  API_BASE_URL?: string;
  SENTRY_DSN?: string;
  OTEL_ENDPOINT?: string;
  FEATURES?: FeatureFlags;
}

declare global {
  interface Window {
    __ENV__?: RuntimeEnv;
  }
}

export function getRuntimeEnv(): RuntimeEnv {
  if (typeof window === 'undefined') return {};
  return window.__ENV__ ?? {};
}

export function getFeatureFlags(): FeatureFlags {
  return getRuntimeEnv().FEATURES ?? {};
}
