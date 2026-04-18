// LoginForm (FE-TASK-029) — презентационная форма логина.
//
// Архитектура:
//  * React Hook Form + zodResolver — клиентская валидация без лишних рендеров.
//  * Передаётся `onSubmit: (values) => Promise<void>` — FSD-граница: feature не
//    знает про auth-flow (processes) и не вызывает API напрямую. Всю авторизацию
//    (POST /auth/login, setAccess, setRefreshToken, GET /users/me) делает
//    pages/auth/LoginPage через processes/auth-flow.login.
//  * Server-side ошибки:
//      - VALIDATION_ERROR с details.fields → applyValidationErrors проставляет
//        inline-ошибки полей (§20.4a).
//      - 401 invalid credentials / иные — формируется form-level сообщение
//        через toUserMessage (§20.4), password очищается для повторной попытки.
//      - REQUEST_ABORTED — игнорируется (unmount во время submit'а).
//  * A11y: `aria-invalid`, `aria-describedby` с message-хинтом ошибки,
//    role=alert для form-level сообщений, focus-ring, видимые labels.
import { zodResolver } from '@hookform/resolvers/zod';
import { type FormEvent, useCallback, useEffect, useId } from 'react';
import { type SubmitHandler, useForm } from 'react-hook-form';

import {
  applyValidationErrors,
  isOrchestratorError,
  toUserMessage,
  type UseFormSetErrorLike,
} from '@/shared/api';
import { Button, Input, Label } from '@/shared/ui';

import { type LoginFormValues,loginSchema } from '../model/schema';

export interface LoginFormProps {
  /**
   * Асинхронный обработчик отправки формы. Должен отклоняться `OrchestratorError`
   * при серверной ошибке (401/VALIDATION_ERROR/…). Успех — resolve без данных;
   * навигация выполняется вызывающей страницей.
   */
  onSubmit: (values: LoginFormValues) => Promise<void>;
  /**
   * Предзаполненный email (например, при переходе с регистрации или из
   * сохранённой сессии). Пароль никогда не предзаполняется.
   */
  defaultEmail?: string;
  /**
   * Текст подсказки под формой. В v1 — ссылка на восстановление/регистрацию —
   * отсутствует (нет экранов). Слот оставлен как расширение; страница сама
   * решает, передавать ли `footerSlot`.
   */
  footerSlot?: React.ReactNode;
  className?: string;
}

/**
 * Form-level-ошибка хранится в `form.formState.errors.root.serverError` через
 * `form.setError('root.serverError', …)` — идиома RHF для form-level-месседжей.
 * В RTL-тестах читаем по role="alert".
 */
const FORM_ERROR_PATH = 'root.serverError' as const;

export function LoginForm({
  onSubmit,
  defaultEmail = '',
  footerSlot,
  className,
}: LoginFormProps): JSX.Element {
  const form = useForm<LoginFormValues>({
    resolver: zodResolver(loginSchema),
    mode: 'onBlur',
    defaultValues: { email: defaultEmail, password: '' },
  });

  const { register, handleSubmit, formState, setError, clearErrors, setValue, setFocus } = form;
  const { errors, isSubmitting } = formState;

  // Программный autofocus на email при mount — альтернатива jsx-атрибуту
  // `autoFocus` (запрещён jsx-a11y/no-autofocus). Если передан `defaultEmail`
  // (предзаполненный сценарий) — фокусим пароль, иначе email.
  useEffect(() => {
    setFocus(defaultEmail ? 'password' : 'email');
  }, [defaultEmail, setFocus]);

  const emailHintId = useId();
  const passwordHintId = useId();
  const formErrorId = useId();
  const formErrorMessage = errors.root?.serverError?.message;

  const submit = useCallback<SubmitHandler<LoginFormValues>>(
    async (values) => {
      // На новой попытке гасим предыдущие server-ошибки — RHF не делает это
      // автоматически (mode=onBlur/onChange не триггерит revalidation root-кодов).
      clearErrors(FORM_ERROR_PATH);
      try {
        await onSubmit(values);
      } catch (err) {
        // REQUEST_ABORTED — пользователь unmounted форму во время запроса:
        // пропускаем тихо, чтобы не флудить сообщениями.
        if (isOrchestratorError(err) && err.error_code === 'REQUEST_ABORTED') return;

        // 1. VALIDATION_ERROR → inline-ошибки полей (auto-focus на первое
        // сматчившееся поле). Unmatched — в form-level banner.
        // `setError` из RHF принимает options-объект с required `shouldFocus`
        // (под exactOptionalPropertyTypes), UseFormSetErrorLike — optional.
        // Сигнатуры структурно совместимы в рантайме → узко кастуем.
        const setErrorCompat = setError as unknown as UseFormSetErrorLike<LoginFormValues>;
        const { matched, unmatched } = applyValidationErrors<LoginFormValues>(err, setErrorCompat);

        if (matched === 0 || unmatched.length > 0) {
          // 2. Не валидационная ошибка / unmatched — form-level сообщение.
          // UX: при 401/«неверный логин или пароль» — чистим password,
          // оставляем email, чтобы упростить повтор.
          const { title } = toUserMessage(err);
          setError(FORM_ERROR_PATH, { type: 'server', message: title });
          setValue('password', '', { shouldDirty: false, shouldTouch: false });
        }
      }
    },
    [clearErrors, onSubmit, setError, setValue],
  );

  // Оборачиваем handleSubmit в нативный-совместимый wrapper, чтобы RHF Zod
  // clientside-валидация сработала ДО server-запроса. noValidate отключает
  // browser-уровневые тултипы (они визуально конфликтуют с нашим UI).
  const onFormSubmit = (e: FormEvent<HTMLFormElement>): void => {
    void handleSubmit(submit)(e);
  };

  return (
    <form
      noValidate
      className={['flex flex-col gap-5', className ?? ''].join(' ')}
      onSubmit={onFormSubmit}
      aria-label="Форма входа"
      data-testid="login-form"
    >
      <div className="flex flex-col gap-2">
        <Label htmlFor="login-email" required>
          Email
        </Label>
        <Input
          id="login-email"
          type="email"
          autoComplete="email"
          error={!!errors.email}
          aria-describedby={errors.email ? emailHintId : undefined}
          {...register('email')}
        />
        {errors.email?.message ? (
          // role="alert" намеренно отсутствует: inline-хинт у поля уже
          // связан через aria-describedby+aria-invalid и анонсируется
          // скрин-ридером при фокусе поля. Живой alert только на form-level
          // banner ниже (review-nit react-specialist).
          <p id={emailHintId} className="text-xs text-danger">
            {errors.email.message}
          </p>
        ) : null}
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="login-password" required>
          Пароль
        </Label>
        <Input
          id="login-password"
          type="password"
          autoComplete="current-password"
          error={!!errors.password}
          aria-describedby={errors.password ? passwordHintId : undefined}
          {...register('password')}
        />
        {errors.password?.message ? (
          <p id={passwordHintId} className="text-xs text-danger">
            {errors.password.message}
          </p>
        ) : null}
      </div>

      {formErrorMessage ? (
        <div
          id={formErrorId}
          role="alert"
          className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger"
          data-testid="login-form-error"
        >
          {formErrorMessage}
        </div>
      ) : null}

      <Button
        type="submit"
        variant="primary"
        size="lg"
        fullWidth
        loading={isSubmitting}
        disabled={isSubmitting}
        aria-describedby={formErrorMessage ? formErrorId : undefined}
      >
        Войти
      </Button>

      {footerSlot}
    </form>
  );
}
