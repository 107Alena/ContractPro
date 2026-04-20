export {
  buildOtelConfig,
  enrichSpan,
  initOtel,
  type InitOtelResult,
  type OtelProviderDescriptor,
  tagXhrCorrelationId,
} from './otel';
export { initSentry, type InitSentryResult, Sentry } from './sentry';
