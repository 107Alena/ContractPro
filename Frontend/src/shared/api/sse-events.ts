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

/** Альтернативный тип договора, предложенный LIC. */
export interface TypeAlternative {
  /** Идентификатор типа договора (whitelist LIC: услуги, поставка, подряд, ...). */
  contract_type: string;
  /** Уверенность модели в этом типе (0.0–1.0). */
  confidence: number;
}

/**
 * Payload SSE-события `type_confirmation_required` (FR-2.1.3). Шлётся, когда
 * LIC классифицировал тип договора с уверенностью ниже `threshold` — версия
 * остановлена в `AWAITING_USER_INPUT`, требуется выбор пользователя.
 *
 * Контракт описан в `ApiBackendOrchestrator/architecture/event-catalog.md`
 * §2.2 (ClassificationUncertain → SSE push). Намеренно НЕ наследует
 * `StatusEvent`: это другой `event_type` с гарантированным набором полей,
 * объединение через optional поля скрыло бы инвариант "если статус
 * AWAITING_USER_INPUT — suggested_type/confidence/threshold present".
 */
export interface TypeConfirmationEvent {
  /** UUID договора. */
  document_id: string;
  /** UUID версии. */
  version_id: string;
  /** Всегда `AWAITING_USER_INPUT` — backend указывает явно для синхронизации UI. */
  status: 'AWAITING_USER_INPUT';
  /** Тип, который LIC считает наиболее вероятным (топ-1 кандидат). */
  suggested_type: string;
  /** Уверенность модели в `suggested_type` (0.0–1.0). По условию ниже `threshold`. */
  confidence: number;
  /** Порог уверенности, ниже которого требуется подтверждение пользователя. */
  threshold: number;
  /** Альтернативные типы (top-N, отсортированы по убыванию confidence).
   *  Backend гарантирует поле, но может прислать пустой массив. */
  alternatives?: TypeAlternative[];
  /** ISO-8601 — время публикации события. */
  timestamp?: string;
  /** X-Correlation-Id запроса, породившего событие (§7.8). */
  correlation_id?: string;
}
