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
export function sanitizeRedirect(raw: string | null, fallback: string = DEFAULT_REDIRECT): string {
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
    <div className="flex min-h-screen flex-col bg-bg text-fg">
      <AuthHeader />
      <div className="flex flex-1 flex-col md:grid md:grid-cols-[minmax(360px,62%)_1fr]">
        <PromoSidebar />
        <main
          data-testid="page-login"
          className="flex items-center justify-center border-l border-border-subtle px-6 py-12 md:px-10 md:py-16"
        >
          <div className="flex w-full max-w-[400px] flex-col gap-6">
            <header className="flex flex-col gap-2">
              <h1 className="text-[28px] font-bold leading-tight text-fg">Вход в ContractPro</h1>
              <p className="text-15 leading-[22px] text-fg-muted">
                Введите данные для входа в рабочее пространство
              </p>
            </header>

            <LoginForm onSubmit={handleSubmit} />

            <div className="flex items-center justify-center gap-1.5 text-12 text-fg-disabled">
              <span aria-hidden="true">🔒</span>
              <span>Защищённое соединение. Данные передаются в зашифрованном виде</span>
            </div>
          </div>
        </main>
      </div>
    </div>
  );
}

// AuthHeader — Figma node 50:2. Минимальная верхняя панель с лого и nav.
// Отличается от LandingHeader: меньше высоты (64px), nav-ссылки маркетинг-only,
// не sticky.
function AuthHeader(): JSX.Element {
  return (
    <header className="flex h-16 shrink-0 items-center justify-between border-b border-divider bg-bg px-6 md:px-20">
      <a
        href="/"
        className="flex items-center gap-2 focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2"
        aria-label="ContractPro главная"
      >
        <span
          aria-hidden="true"
          className="flex h-7 items-center justify-center rounded-md bg-brand-600 px-1.5 text-16 font-bold text-white"
        >
          C
        </span>
        <span className="text-20 font-bold text-fg">ContractPro</span>
      </a>
      <nav aria-label="Дополнительные ссылки" className="hidden items-center gap-8 text-14 md:flex">
        <a className="font-medium text-fg-muted hover:text-fg" href="/">
          На главную
        </a>
        <a className="font-medium text-fg-muted hover:text-fg" href="mailto:support@contractpro.ru">
          Нужна помощь?
        </a>
        <a className="font-medium text-brand-600 hover:text-brand-500" href="/">
          Запросить демо
        </a>
      </nav>
    </header>
  );
}
