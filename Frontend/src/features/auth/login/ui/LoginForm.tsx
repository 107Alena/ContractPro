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
import { type FormEvent, useCallback, useEffect, useId, useState } from 'react';
import { type SubmitHandler, useForm } from 'react-hook-form';

import {
  applyValidationErrors,
  isOrchestratorError,
  toUserMessage,
  type UseFormSetErrorLike,
} from '@/shared/api';
import { Button, Input, Label } from '@/shared/ui';

import { type LoginFormValues, loginSchema } from '../model/schema';

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

  // Toggle visibility пароля (Figma node 56:16 — eye icon в правой части поля).
  const [showPassword, setShowPassword] = useState(false);

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
          Рабочий email
        </Label>
        <Input
          id="login-email"
          type="email"
          autoComplete="email"
          placeholder="name@company.ru"
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
        <div className="relative">
          <Input
            id="login-password"
            type={showPassword ? 'text' : 'password'}
            autoComplete="current-password"
            placeholder="Введите пароль"
            className="pr-11"
            error={!!errors.password}
            aria-describedby={errors.password ? passwordHintId : undefined}
            {...register('password')}
          />
          <button
            type="button"
            onClick={() => setShowPassword((v) => !v)}
            // Без слова «пароль» — иначе getByLabelText(/пароль/i) в тестах
            // натыкается на две метки. Контекст очевиден из позиции внутри
            // password-input + смены иконки глаза.
            aria-label={showPassword ? 'Скрыть' : 'Показать'}
            aria-pressed={showPassword}
            className="absolute right-2 top-1/2 inline-flex h-7 w-7 -translate-y-1/2 items-center justify-center rounded-sm text-fg-disabled hover:text-fg-muted focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1"
          >
            <EyeIcon open={showPassword} />
          </button>
        </div>
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

function EyeIcon({ open }: { open: boolean }): JSX.Element {
  // Открытый глаз — пароль виден; перечёркнутый — скрыт.
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      width="18"
      height="18"
      viewBox="0 0 20 20"
      fill="none"
    >
      {open ? (
        <>
          <path
            d="M10 4.5c-4 0-7 3-8.5 5.5C3 12.5 6 15.5 10 15.5s7-3 8.5-5.5C17 7.5 14 4.5 10 4.5z"
            stroke="currentColor"
            strokeWidth="1.5"
          />
          <circle cx="10" cy="10" r="2.5" stroke="currentColor" strokeWidth="1.5" />
        </>
      ) : (
        <>
          <path
            d="M3 3l14 14M7.6 7.6A2.5 2.5 0 0 0 12.4 12.4M5.4 5.7C3.6 6.9 2.2 8.6 1.5 10c1.5 2.5 4.5 5.5 8.5 5.5 1.4 0 2.7-.4 3.8-.9M9 4.6c.3 0 .7-.1 1-.1 4 0 7 3 8.5 5.5-.5.8-1.2 1.8-2.2 2.7"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
          />
        </>
      )}
    </svg>
  );
}
