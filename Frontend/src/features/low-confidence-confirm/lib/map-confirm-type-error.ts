// Маппер ошибок POST /confirm-type → action для UI.
//
// Решения по кодам:
// - VERSION_NOT_AWAITING_INPUT (409): модалка устарела — закрываем и
//   показываем toast.warning. Пользователю нечего исправлять, состояние
//   протухло (другая сессия подтвердила, watchdog завершил версию или анализ
//   уже идёт). Архитектурный гайд: error-handling.md §VERSION_NOT_AWAITING_INPUT.
// - VALIDATION_ERROR / 400 INVALID contract_type: пользователь выбрал что-то
//   нерегламентное → подсказка остаётся в форме, модалка не закрывается.
//   Подсветка через setError на поле `contract_type`.
// - REQUEST_ABORTED: пользователь отменил (закрыл модалку) — без UX.
// - Остальные коды (401/403/404/5xx): фолбэк toast.error через page-level
//   onError-callback (`useConfirmType` пробрасывает в опциональный `onError`).
import type { OrchestratorError } from '@/shared/api';

export type ConfirmTypeAction =
  | { kind: 'stale' } // 409 — закрыть модалку + toast.warning
  | { kind: 'invalid-type'; message: string } // 400 — оставить модалку, setError
  | { kind: 'aborted' } // отмена
  | { kind: 'unknown' }; // фолбэк → page toast.error

export const STALE_TOAST_TITLE = 'Подтверждение типа уже не требуется';
export const STALE_TOAST_HINT =
  'Обновите страницу — актуальный статус доступен в реальном времени.';

export function mapConfirmTypeError(err: OrchestratorError): ConfirmTypeAction {
  if (err.error_code === 'REQUEST_ABORTED') return { kind: 'aborted' };
  if (err.error_code === 'VERSION_NOT_AWAITING_INPUT') return { kind: 'stale' };
  // 400 INVALID contract_type приходит как VALIDATION_ERROR (по архитектуре
  // §400 confirm-type) или как самостоятельный код (зависит от backend
  // нормализации). Покрываем оба.
  if (err.error_code === 'VALIDATION_ERROR') {
    return { kind: 'invalid-type', message: err.message || 'Недопустимый тип договора.' };
  }
  return { kind: 'unknown' };
}
