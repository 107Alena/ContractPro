// LoginPage (FE-TASK-029) — экран /login.
//
// Архитектура:
//  * Figma «Auth Page (Desktop + Mobile)» (§17.4): 2-колоночный layout — слева
//    `PromoSidebar` (скрыт <md), справа `LoginForm`. Центрирование формы по
//    вертикали, max-width 25rem (шаблон Figma).
//  * API-поток: POST /auth/login → setAccess + setRefreshToken → GET /users/me
//    — всё инкапсулировано в `processes/auth-flow.login()`. Page вызывает его
//    и при успехе выполняет `navigate(redirect ?? '/dashboard', replace=true)`
//    (§5.1, §5.3, §6.1).
//  * Redirect параметр: читается из `?redirect=<path>`; только same-origin
//    path'ы (начинающиеся с '/'), иначе — fallback на '/dashboard'. Это
//    защищает от open-redirect (OWASP A01:2021).
//  * Уже-аутентифицированный пользователь: если в сторе уже есть accessToken,
//    сразу редиректим на dashboard (нет смысла показывать форму повторно).
//  * Ошибки: форма обрабатывает VALIDATION_ERROR и form-level ошибки сама;
//    page-level ловля нужна только для success-path (redirect).
import { useCallback, useEffect } from 'react';
import { Navigate, useNavigate, useSearchParams } from 'react-router-dom';

import { LoginForm, type LoginFormValues } from '@/features/auth/login';
import { login } from '@/processes/auth-flow';
import { useIsAuthenticated } from '@/shared/auth';
import { PromoSidebar } from '@/widgets/promo-sidebar';

// Литералы вместо импорта @/app/router.ROUTES — `pages` не может зависеть от
// `app`-layer. Синхронизировано с `src/app/router/router.tsx` (§6.1).
const DEFAULT_REDIRECT = '/dashboard';
const LOGIN_PATH = '/login';

/**
 * Безопасная нормализация ?redirect=...: допускаем только относительные
 * path'ы ('/...'), без protocol-relative URL ('//evil.com') и без абсолютных
 * ('https://...'). Такой параметр мог прийти из интерсептора 401-redirect
 * (§5.3 «/login?redirect=<current>»).
 */
export function sanitizeRedirect(
  raw: string | null,
  fallback: string = DEFAULT_REDIRECT,
): string {
  if (!raw) return fallback;
  // Запрещаем: absolute URL, protocol-relative, empty, backslash-trick.
  if (!raw.startsWith('/') || raw.startsWith('//') || raw.startsWith('/\\')) {
    return fallback;
  }
  // Так же блокируем повторный /login (иначе — loop после soft-logout).
  if (raw === LOGIN_PATH || raw.startsWith(`${LOGIN_PATH}?`)) return fallback;
  return raw;
}

export function LoginPage(): JSX.Element {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const isAuthenticated = useIsAuthenticated();

  const redirectTo = sanitizeRedirect(searchParams.get('redirect'));

  // Если пользователь уже авторизован — не показываем форму (например,
  // прямой переход на /login при активной сессии).
  useEffect(() => {
    if (isAuthenticated) navigate(redirectTo, { replace: true });
  }, [isAuthenticated, navigate, redirectTo]);

  const handleSubmit = useCallback(
    async (values: LoginFormValues): Promise<void> => {
      // processes/auth-flow.login бросает OrchestratorError при 401/VALIDATION_ERROR/…
      // — их ловит LoginForm и маппит в UI. При успехе — редирект.
      await login({ email: values.email, password: values.password });
      navigate(redirectTo, { replace: true });
    },
    [navigate, redirectTo],
  );

  if (isAuthenticated) {
    // Не рендерим форму, пока эффект выше не сработал. `Navigate` работает
    // даже без эффекта (SSR-safe).
    return <Navigate to={redirectTo} replace />;
  }

  return (
    <div className="min-h-screen bg-bg text-fg md:grid md:grid-cols-[minmax(360px,45%)_1fr]">
      <PromoSidebar />
      <main
        data-testid="page-login"
        className="flex min-h-screen items-center justify-center px-6 py-12 md:px-10 md:py-16"
      >
        <div className="flex w-full max-w-sm flex-col gap-8">
          <header className="flex flex-col gap-2">
            <MobileBrandMark className="md:hidden" />
            <h1 className="text-2xl font-semibold text-fg md:text-3xl">Вход в&nbsp;ContractPro</h1>
            <p className="text-sm text-fg-muted">
              Введите email и пароль, выданные администратором организации.
            </p>
          </header>

          <LoginForm onSubmit={handleSubmit} />

          <p className="text-xs text-fg-muted">
            Пользуясь сервисом, вы соглашаетесь с политикой обработки данных.
            ContractPro не заменяет консультацию юриста.
          </p>
        </div>
      </main>
    </div>
  );
}

function MobileBrandMark({ className }: { className?: string }): JSX.Element {
  return (
    <div className={['flex items-center gap-2 text-lg font-semibold', className ?? ''].join(' ')}>
      <span
        aria-hidden="true"
        className="inline-flex h-8 w-8 items-center justify-center rounded-md bg-brand-500 text-white"
      >
        CP
      </span>
      ContractPro
    </div>
  );
}
