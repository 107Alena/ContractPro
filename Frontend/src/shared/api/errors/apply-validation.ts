// Маппинг VALIDATION_ERROR на форму (§20.4a high-architecture).
//
// Контракт backend — `ValidationErrorDetails { fields: ValidationFieldError[] }`
// из openapi.d.ts. Хелпер:
//   1. проверяет что err — OrchestratorError с code === 'VALIDATION_ERROR',
//   2. для каждой f из details.fields вызывает setError(f.field, { type: f.code, message }),
//   3. accept-focus переносится на ПЕРВОЕ невалидное поле (UX-guideline),
//   4. поля, которых нет в форме (setError бросает), собираются в `unmatched`
//      — вызывающий обычно показывает их как toast-ошибку общей формы.
//
// i18n: задача FE-TASK-014 поставляется ДО FE-TASK-030 (i18next setup). Поэтому
// хелпер принимает OPTIONAL `translate`-функцию. Default: возвращает серверный
// `message` как финальный текст. После FE-TASK-030 вызывающие передают
// `(code, fallback, params) => i18n.t('validation.' + code, { defaultValue: fallback, ...params })`
// — так достигается «i18n приоритетнее серверного message» из §20.4a.
import type { components } from '@/shared/api/openapi';

import type { OrchestratorError } from './orchestrator-error';
import { isOrchestratorError } from './orchestrator-error';

export type ValidationFieldError = components['schemas']['ValidationFieldError'];
export type ValidationErrorDetails = components['schemas']['ValidationErrorDetails'];

/**
 * Структурный тип, совместимый с `UseFormSetError<T>` из react-hook-form,
 * но не требующий rhf-зависимости. Рефактор на настоящий `UseFormSetError`
 * тривиален — сигнатуры идентичны (см. https://react-hook-form.com/docs/useform/seterror).
 */
export type UseFormSetErrorLike<TFieldValues extends FieldValuesLike = FieldValuesLike> = (
  name: keyof TFieldValues & string,
  error: { type: string; message: string },
  options?: { shouldFocus?: boolean },
) => void;

export type FieldValuesLike = Record<string, unknown>;

/**
 * Функция-переводчик для i18n-интеграции. Должна быть синхронной (форма
 * рендерится в тот же tick). `params` — данные из `ValidationFieldError.params`
 * (например, `{ max: 100 }` для `TOO_LONG`). Возвращает строку для `setError`.
 */
export type TranslateFn = (
  code: ValidationFieldError['code'],
  fallback: string,
  params: Record<string, unknown>,
) => string;

export interface ApplyValidationResult {
  /** Количество полей, успешно помеченных в форме. */
  matched: number;
  /**
   * Поля, не сматчившиеся на форму (setError бросил, либо форма их не знает).
   * Вызывающий обычно агрегирует их в toast или form-level banner.
   */
  unmatched: ValidationFieldError[];
}

/**
 * Маппит `VALIDATION_ERROR.details.fields` на `form.setError`.
 *
 * @param err произвольное значение (обычно из catch/onError).
 * @param setError структурно-совместимая функция установки ошибки поля.
 * @param translate опционально — превращает (code, fallback, params) → message.
 *     Default: fallback (серверный message).
 *
 * Invariant: не бросает. Не-OrchestratorError / не-VALIDATION_ERROR → `{0, []}`.
 */
export function applyValidationErrors<T extends FieldValuesLike = FieldValuesLike>(
  err: unknown,
  setError: UseFormSetErrorLike<T>,
  translate?: TranslateFn,
): ApplyValidationResult {
  if (!isValidationError(err)) {
    return { matched: 0, unmatched: [] };
  }

  const fields = readFields(err);
  let matched = 0;
  const unmatched: ValidationFieldError[] = [];

  for (const f of fields) {
    const params = (f.params ?? {}) as Record<string, unknown>;
    const message = translate ? translate(f.code, f.message, params) : f.message;
    try {
      // shouldFocus: true только для первого сматчившегося — UX-guideline «auto-focus
      // на первую невалидную». RHF сам проигнорирует focus, если поле unmounted.
      setError(
        f.field as keyof T & string,
        { type: f.code, message },
        { shouldFocus: matched === 0 },
      );
      matched++;
    } catch {
      // setError бросает, если поле не существует в форме — не прокатываем наверх,
      // а складываем в unmatched для toast-уровневой обработки.
      unmatched.push(f);
    }
  }

  return { matched, unmatched };
}

/** Узкий type-guard: OrchestratorError с кодом VALIDATION_ERROR. */
export function isValidationError(err: unknown): err is OrchestratorError {
  return isOrchestratorError(err) && err.error_code === 'VALIDATION_ERROR';
}

/**
 * Безопасно извлекает `fields` из `err.details`. `details` в OpenAPI-схеме
 * типизирован как `Record<string, never> | null` (openapi-typescript не умеет
 * в дискриминантные union'ы по коду), поэтому narrow-им вручную.
 */
function readFields(err: OrchestratorError): ValidationFieldError[] {
  const details = err.details as unknown;
  if (!details || typeof details !== 'object') return [];
  const maybeFields = (details as { fields?: unknown }).fields;
  return Array.isArray(maybeFields) ? (maybeFields as ValidationFieldError[]) : [];
}
