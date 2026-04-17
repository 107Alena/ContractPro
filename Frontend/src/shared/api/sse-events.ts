// Типы SSE-событий (§7.7/§20.2 high-architecture.md). Отдельный файл —
// потому что владельцем контракта SSE является транспортный слой shared/api
// (§7.7), но entities/job по §20.2 предоставляет re-export для удобного
// импорта из features/widgets/pages. FSD boundaries запрещают
// shared -> entities, поэтому сам тип живёт здесь.
import type { components } from './openapi';

export type UserProcessingStatus = components['schemas']['UserProcessingStatus'];

/**
 * Payload SSE-события `status_update`. Backend шлёт при каждой смене статуса
 * обработки версии. `heartbeat` отправляется SSE-комментом `:ping` —
 * EventSource не пропускает его к listener'ам и в типе отсутствует.
 */
export interface StatusEvent {
  /** UUID версии, к которой относится событие. */
  version_id: string;
  /** UUID договора (в DM — document_id, в Orchestrator API — contract_id). */
  document_id: string;
  /** Текущий user-facing статус обработки. */
  status: UserProcessingStatus;
  /** Статус на русском (по §11.1). */
  message?: string;
  /** Прогресс 0..100 (опционально). SSE-only — `ProcessingStatus` REST-ответ
   *  этого поля не содержит, поэтому при polling-fallback оно undefined. */
  progress?: number;
  /** ISO-8601 — время публикации события. */
  timestamp?: string;
  /** X-Correlation-Id запроса, породившего событие (§7.8). */
  correlation_id?: string;
}
