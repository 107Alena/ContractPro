// Хелпер для отличения 404 DIFF_NOT_FOUND ("сравнение ещё не готово") от
// остальных ошибок GET /diff.
//
// UX-контракт (§9.3 / catalog): DIFF_NOT_FOUND — soft-state, страница показывает
// message "Сравнение ещё не готово" (или empty-state с кнопкой "Дождаться"),
// а не toast-ошибку. Остальные ошибки (VERSION_NOT_FOUND, PERMISSION_DENIED,
// INTERNAL_ERROR) идут через toUserMessage → toast.
//
// Держим как отдельную функцию: вызывается и в useDiff (retry-predicate), и
// в ResultPage (UI-switch). Единая точка правды — код не разойдётся.
import { isOrchestratorError } from '@/shared/api';

/**
 * Возвращает true, если ошибка — OrchestratorError с кодом DIFF_NOT_FOUND
 * (HTTP 404 от GET /diff). В этом случае UI показывает soft-message
 * "Сравнение ещё не готово" вместо error-toast.
 */
export function isDiffNotReadyError(err: unknown): boolean {
  return isOrchestratorError(err) && err.error_code === 'DIFF_NOT_FOUND';
}
