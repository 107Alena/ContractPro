// toUserMessage: универсальный error-to-UI преобразователь (§20.4 high-architecture).
//
// Приоритет источников title (по убыванию):
//   1. серверный OrchestratorError.message (backend-контракт — уже на русском, NFR-5.2)
//   2. ERROR_UX[code].title (fallback по каталогу §7.3)
//   3. navigator.onLine === false → «Нет соединения с интернетом»
//   4. «Непредвиденная ошибка» (unknown-shape, не Orchestrator, не offline)
//
// hint берётся из err.suggestion, затем ERROR_UX[code].hint.
// action — только из ERROR_UX (backend не транслирует UX-решения).
import { ERROR_UX } from './catalog';
import { type ErrorAction, isKnownErrorCode } from './codes';
import { isOrchestratorError } from './orchestrator-error';

export interface UserMessage {
  title: string;
  hint?: string;
  action?: ErrorAction;
  correlationId?: string;
}

/**
 * Преобразует произвольный `err` (обычно из catch/onError TanStack Query)
 * в структуру, готовую к отображению (toast / dialog / inline-banner).
 *
 * Никогда не бросает: в контекстах `onError` / SSE-handlers любое throw
 * приведёт к потере корреляционного ID и возможному крашу boundary.
 */
export function toUserMessage(err: unknown): UserMessage {
  if (isOrchestratorError(err)) {
    // Runtime narrow через isKnownErrorCode — избегаем `as ErrorCode`-каста.
    // Неизвестный код (backend добавил новый, фронт ещё не обновлён) → generic fallback.
    const ux = isKnownErrorCode(err.error_code) ? ERROR_UX[err.error_code] : undefined;
    const fallback = { title: 'Произошла ошибка' } as const;
    const entry = ux ?? fallback;

    // title: серверный message имеет приоритет (архитектурный инвариант NFR-5.2).
    // Но если backend прислал пустую строку — откатываемся на каталог.
    const title = err.message && err.message.trim() !== '' ? err.message : entry.title;

    // hint: серверный suggestion приоритетнее, но только если он непустой.
    // null/undefined/'' → fallback на каталог.
    const suggestion =
      typeof err.suggestion === 'string' && err.suggestion.trim() !== ''
        ? err.suggestion
        : undefined;
    const catalogHint = 'hint' in entry ? entry.hint : undefined;
    const catalogAction = 'action' in entry ? entry.action : undefined;

    const msg: UserMessage = { title };
    const hint = suggestion ?? catalogHint;
    if (hint !== undefined) msg.hint = hint;
    if (catalogAction !== undefined) msg.action = catalogAction;
    if (err.correlationId !== undefined) msg.correlationId = err.correlationId;
    return msg;
  }

  // navigator может отсутствовать в node-окружении (Vitest env=node) —
  // проверяем явно, чтобы не бросать ReferenceError в тестах на unit-уровне.
  if (typeof navigator !== 'undefined' && navigator.onLine === false) {
    return { title: 'Нет соединения с интернетом', action: 'retry' };
  }

  return { title: 'Непредвиденная ошибка', action: 'retry' };
}
