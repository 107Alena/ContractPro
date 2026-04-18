// Type-guard: «отчёт ещё не готов» (§7.3 error catalog, §17.5 artifact table).
//
// 404 ARTIFACT_NOT_FOUND / RESULTS_NOT_READY — не «ошибка навсегда», а «пока
// не готово». UI показывает пользователю дружественное сообщение
// «Результат ещё не готов» и disable'ит кнопку экспорта.
import { isOrchestratorError, type OrchestratorError } from '@/shared/api';

export function isExportNotReadyError(err: unknown): err is OrchestratorError {
  if (!isOrchestratorError(err)) return false;
  return err.error_code === 'ARTIFACT_NOT_FOUND' || err.error_code === 'RESULTS_NOT_READY';
}
