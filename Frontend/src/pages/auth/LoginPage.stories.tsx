// LoginPage.stories — 4 состояния из AC FE-TASK-029
// (Default / Loading / ServerError / ValidationError).
//
// `onSubmit` подменяется модулем-мок-обёрткой через prop `_submitOverride` —
// это не production-API, а storybook-testing hook. Page в production использует
// processes/auth-flow.login. Для сториз мы рендерим LoginForm напрямую поверх
// layout'а страницы, чтобы не завязываться на TanStack Query/Router/axios.
//
// Декоратор MemoryRouter нужен LoginPage (useNavigate/useSearchParams) —
// прямое использование Page-компонента без роутера упадёт в Storybook.
import type { Meta, StoryObj } from '@storybook/react';
import { MemoryRouter } from 'react-router-dom';

import { LoginForm, type LoginFormValues } from '@/features/auth/login';
import { OrchestratorError } from '@/shared/api';
import { PromoSidebar } from '@/widgets/promo-sidebar';

const meta: Meta = {
  title: 'Pages/LoginPage',
  parameters: { layout: 'fullscreen' },
  decorators: [
    (Story): JSX.Element => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};

export default meta;

type Story = StoryObj;

function PageShell({
  onSubmit,
  defaultEmail = '',
}: {
  onSubmit: (values: LoginFormValues) => Promise<void>;
  defaultEmail?: string;
}): JSX.Element {
  return (
    <div className="min-h-screen bg-bg text-fg md:grid md:grid-cols-[minmax(360px,45%)_1fr]">
      <PromoSidebar />
      <main className="flex min-h-screen items-center justify-center px-6 py-12 md:px-10 md:py-16">
        <div className="flex w-full max-w-sm flex-col gap-8">
          <header className="flex flex-col gap-2">
            <h1 className="text-2xl font-semibold text-fg md:text-3xl">
              Вход в ContractPro
            </h1>
            <p className="text-sm text-fg-muted">
              Введите email и пароль, выданные администратором организации.
            </p>
          </header>
          <LoginForm onSubmit={onSubmit} defaultEmail={defaultEmail} />
        </div>
      </main>
    </div>
  );
}

export const Default: Story = {
  render: () => (
    <PageShell
      onSubmit={async () => {
        // noop: в Storybook форма «успешно» отправляется без редиректа.
        await new Promise((res) => setTimeout(res, 200));
      }}
    />
  ),
};

export const Loading: Story = {
  render: () => (
    <PageShell
      defaultEmail="maria@example.ru"
      onSubmit={async () => {
        // Никогда не резолвится — демонстрируем спиннер/disabled-state
        // на кнопке «Войти». В Chromatic зафиксирует кадр с loading=true.
        await new Promise(() => {});
      }}
    />
  ),
  parameters: { chromatic: { delay: 300 } },
};

export const ServerError: Story = {
  render: () => (
    <PageShell
      defaultEmail="maria@example.ru"
      onSubmit={async () => {
        await new Promise((res) => setTimeout(res, 100));
        throw new OrchestratorError({
          error_code: 'AUTH_TOKEN_INVALID',
          message: 'Неверный email или пароль',
          status: 401,
        });
      }}
    />
  ),
  parameters: { chromatic: { delay: 500 } },
};

export const ValidationError: Story = {
  render: () => (
    <PageShell
      defaultEmail="user@domain.ru"
      onSubmit={async () => {
        await new Promise((res) => setTimeout(res, 100));
        throw new OrchestratorError({
          error_code: 'VALIDATION_ERROR',
          message: 'Ошибка валидации',
          status: 400,
          details: {
            fields: [
              {
                field: 'password',
                code: 'TOO_SHORT',
                message: 'Пароль слишком короткий',
              },
            ],
          } as unknown as OrchestratorError['details'],
        });
      }}
    />
  ),
  parameters: { chromatic: { delay: 500 } },
};
